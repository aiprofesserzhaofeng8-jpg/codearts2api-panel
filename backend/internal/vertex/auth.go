// SPDX-License-Identifier: LGPL-3.0-only
// Adapted from Sub2API vertex_service_account.go; see UPSTREAM.md.
package vertex

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const tokenURL = "https://oauth2.googleapis.com/token"
const cloudScope = "https://www.googleapis.com/auth/cloud-platform"

type serviceAccount struct {
	Type     string `json:"type"`
	Project  string `json:"project_id"`
	KeyID    string `json:"private_key_id,omitempty"`
	Key      string `json:"private_key"`
	Email    string `json:"client_email"`
	TokenURI string `json:"token_uri,omitempty"`
}
type credentials struct {
	Models   []string       `json:"model_ids,omitempty"`
	Service  serviceAccount `json:"service_account"`
	Project  string         `json:"project_id"`
	Location string         `json:"location"`
	Scope    string         `json:"scope"`
	Access   string         `json:"access_token,omitempty"`
	Expires  int64          `json:"expires_at,omitempty"`
}

// Auth implements the existing credential coordinator contract. It owns no
// cache, lock or persistence; the account service commits the returned token.
type Auth struct{ HTTP *http.Client }

func NewAuth() *Auth { return &Auth{HTTP: providerhttp.New()} }

var projectPattern = regexp.MustCompile(`^(?:[a-z][a-z0-9-]{4,61}[a-z0-9]|[0-9]{1,20})$`)
var locationPattern = regexp.MustCompile(`^(?:global|[a-z]+(?:-[a-z]+)*[0-9])$`)
var modelPattern = regexp.MustCompile(`^gemini-[A-Za-z0-9._-]+$`)

func parsePrivateKey(value string) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, providerhttp.Invalid("Vertex AI", "服务账号 RSA 私钥无效")
	}
	var key *rsa.PrivateKey
	if block.Type == "PRIVATE KEY" {
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			key, _ = parsed.(*rsa.PrivateKey)
		}
	} else if block.Type == "RSA PRIVATE KEY" {
		key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, providerhttp.Invalid("Vertex AI", "服务账号 RSA 私钥无效")
	}
	return key, nil
}
func (a *Auth) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var c credentials
	bad := func() (json.RawMessage, provider.Identity, error) {
		return nil, provider.Identity{}, providerhttp.Invalid("Vertex AI", "需要服务账号 JSON、有效项目和区域")
	}
	if len(raw) > 256<<10 || json.Unmarshal(raw, &c) != nil {
		return bad()
	}
	if c.Service.Type != "service_account" || !projectPattern.MatchString(c.Service.Project) || c.Service.Email == "" || !strings.HasSuffix(c.Service.Email, ".gserviceaccount.com") || !strings.Contains(c.Service.Email, "@") || !providerhttp.Safe(c.Service.Email, c.Service.KeyID, c.Access) {
		return bad()
	}
	if _, err := parsePrivateKey(c.Service.Key); err != nil {
		return nil, provider.Identity{}, err
	}
	if c.Project == "" {
		c.Project = c.Service.Project
	}
	if c.Location == "" {
		c.Location = "us-central1"
	}
	if !projectPattern.MatchString(c.Project) || !locationPattern.MatchString(c.Location) || len(c.Location) > 63 || c.Scope != "" && c.Scope != c.Location || c.Access != "" && c.Expires <= 0 || c.Access == "" && c.Expires != 0 {
		return bad()
	}
	// Imported token_uri never controls the destination of the private assertion.
	c.Service.TokenURI = tokenURL
	c.Scope = c.Location
	sum := sha256.Sum256([]byte(c.Service.Email + "\x00" + c.Project + "\x00" + c.Location))
	b, err := json.Marshal(c)
	return b, provider.Identity{ID: hex.EncodeToString(sum[:16]), Name: "Vertex AI / " + c.Project, Scope: c.Location, ExpiresAt: c.Expires, HasRefresh: true}, err
}
func assertion(c credentials, now time.Time) (string, error) {
	key, err := parsePrivateKey(c.Service.Key)
	if err != nil {
		return "", err
	}
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	if c.Service.KeyID != "" {
		header["kid"] = c.Service.KeyID
	}
	h, _ := json.Marshal(header)
	b, _ := json.Marshal(map[string]any{"iss": c.Service.Email, "scope": cloudScope, "aud": tokenURL, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	unsigned := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(b)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", providerhttp.Broken("Vertex AI")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
func (a *Auth) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	normalized, _, err := a.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var c credentials
	_ = json.Unmarshal(normalized, &c)
	started := time.Now()
	jwt, err := assertion(c, started)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {jwt}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, provider.NewTransportError("Vertex AI", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, provider.NewTransportError("Vertex AI", err)
	}
	if len(body) > 1<<20 {
		return nil, providerhttp.Broken("Vertex AI")
	}
	var result struct {
		Access string `json:"access_token"`
		Type   string `json:"token_type"`
		TTL    int64  `json:"expires_in"`
		Error  string `json:"error"`
	}
	parseErr := json.Unmarshal(body, &result)
	if resp.StatusCode != 200 {
		if resp.StatusCode == 400 && (result.Error == "invalid_grant" || result.Error == "invalid_client") {
			return nil, &provider.Error{Status: 401, AccountFault: true, Message: "Vertex AI 服务账号授权失败，请检查服务账号与密钥"}
		}
		return nil, providerhttp.FailureResponse("Vertex AI", resp)
	}
	if parseErr != nil || result.Error != "" || result.Access == "" || !providerhttp.Safe(result.Access) || !strings.EqualFold(result.Type, "Bearer") || result.TTL <= 0 || result.TTL > 3600 {
		return nil, providerhttp.Broken("Vertex AI")
	}
	c.Access = result.Access
	c.Expires = started.Unix() + result.TTL
	if c.Expires <= time.Now().Unix() {
		return nil, providerhttp.Broken("Vertex AI")
	}
	return json.Marshal(c)
}

// GeminiURL preserves the supplied model and location. No automatic regional
// fallback or model substitution is performed.
func GeminiURL(project, location, model string) (string, error) {
	if !projectPattern.MatchString(project) || !locationPattern.MatchString(location) || len(location) > 63 || !modelPattern.MatchString(model) || len(model) > 256 {
		return "", providerhttp.Invalid("Vertex AI", "无效的项目、区域或 Gemini 型号")
	}
	host := location + "-aiplatform.googleapis.com"
	if location == "global" {
		host = "aiplatform.googleapis.com"
	}
	return "https://" + host + "/v1/projects/" + url.PathEscape(project) + "/locations/" + url.PathEscape(location) + "/publishers/google/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse", nil
}
