// Package kiro implements Kiro's native OAuth, catalog and AWS EventStream protocols.
// It never launches an agent, reads desktop credentials or executes returned tools.
package kiro

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"codearts2api/internal/provider"
)

type Region struct{ Runtime, Catalog, OIDC, Social string }
type Credentials struct {
	AccessToken           string `json:"access_token,omitempty"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	APIKey                string `json:"api_key,omitempty"`
	ExpiresAt             int64  `json:"expires_at,omitempty"`
	AuthMethod            string `json:"auth_method"`
	Region                string `json:"region"`
	AuthRegion            string `json:"auth_region,omitempty"`
	ProfileARN            string `json:"profile_arn,omitempty"`
	ClientID              string `json:"client_id,omitempty"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at,omitempty"`
}
type Client struct {
	HTTP    *http.Client
	Regions map[string]Region
	mu      sync.Mutex
	logins  map[string]*pendingLogin
}

func New() *Client {
	c := &Client{HTTP: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 90 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Regions: map[string]Region{}, logins: map[string]*pendingLogin{}}
	for _, region := range []string{"us-east-1", "eu-central-1"} {
		c.Regions[region] = Region{"https://q." + region + ".amazonaws.com", "https://codewhisperer." + region + ".amazonaws.com", "https://oidc." + region + ".amazonaws.com", "https://prod." + region + ".auth.desktop.kiro.dev"}
	}
	region := c.Regions["eu-central-1"]
	// The CodeWhisperer metadata hostname is not present in every region.
	// Keep EU metadata on the same regional Q service; never cross regions.
	region.Catalog = region.Runtime
	c.Regions["eu-central-1"] = region
	return c
}
func (*Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.Kiro, Name: "Kiro", Available: true, Scopes: []string{"us-east-1", "eu-central-1"}, Login: true}
}
func field(o map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		var s string
		if json.Unmarshal(o[k], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}
func timestamp(b json.RawMessage) int64 {
	var s string
	if json.Unmarshal(b, &s) != nil {
		s = string(b)
	}
	if n, e := strconv.ParseInt(s, 10, 64); e == nil {
		if n > 1e12 {
			n /= 1000
		}
		return n
	}
	if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
		return t.Unix()
	}
	return 0
}

var arnPattern = regexp.MustCompile(`^arn:aws:codewhisperer:([a-z0-9-]+):[0-9]+:profile/[A-Za-z0-9_-]+$`)

func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var o map[string]json.RawMessage
	if json.Unmarshal(raw, &o) != nil || o == nil {
		return nil, provider.Identity{}, fmt.Errorf("Kiro 凭据必须为 JSON 对象")
	}
	a := Credentials{AccessToken: field(o, "access_token", "accessToken"), RefreshToken: field(o, "refresh_token", "refreshToken"), APIKey: field(o, "api_key", "kiroApiKey", "kiro_api_key", "ksk"), AuthMethod: strings.ToLower(field(o, "auth_method", "authMethod")), Region: field(o, "region", "scope"), AuthRegion: field(o, "auth_region", "authRegion"), ProfileARN: field(o, "profile_arn", "profileArn"), ClientID: field(o, "client_id", "clientId"), ClientSecret: field(o, "client_secret", "clientSecret")}
	for _, k := range []string{"expires_at", "expiresAt"} {
		if len(o[k]) > 0 {
			a.ExpiresAt = timestamp(o[k])
			break
		}
	}
	for _, k := range []string{"client_secret_expires_at", "clientSecretExpiresAt"} {
		if len(o[k]) > 0 {
			a.ClientSecretExpiresAt = timestamp(o[k])
			break
		}
	}
	if a.Region == "" {
		a.Region = "us-east-1"
	}
	if a.ProfileARN != "" {
		m := arnPattern.FindStringSubmatch(a.ProfileARN)
		if m == nil {
			return nil, provider.Identity{}, fmt.Errorf("Kiro profileArn 格式无效")
		}
		if field(o, "region", "scope") != "" && a.Region != m[1] {
			return nil, provider.Identity{}, fmt.Errorf("Kiro region 与 profileArn 不一致")
		}
		a.Region = m[1]
	}
	if a.AuthRegion == "" {
		a.AuthRegion = a.Region
	}
	if _, ok := c.Regions[a.Region]; !ok {
		return nil, provider.Identity{}, fmt.Errorf("Kiro region 仅支持 us-east-1 / eu-central-1")
	}
	if _, ok := c.Regions[a.AuthRegion]; !ok {
		return nil, provider.Identity{}, fmt.Errorf("Kiro auth_region 无效")
	}
	if a.APIKey != "" {
		if !strings.HasPrefix(a.APIKey, "ksk_") || a.AccessToken != "" || a.RefreshToken != "" {
			return nil, provider.Identity{}, fmt.Errorf("Kiro API Key 须以 ksk_ 开头，不能与 OAuth 令牌混用")
		}
		a.AuthMethod = "api_key"
		a.ExpiresAt = 0
		a.ClientID = ""
		a.ClientSecret = ""
		a.ClientSecretExpiresAt = 0
	} else {
		if a.AuthMethod == "" {
			if a.ClientID != "" {
				a.AuthMethod = "idc"
			} else {
				a.AuthMethod = "social"
			}
		}
		if a.AccessToken == "" || a.ExpiresAt <= 0 || a.AuthMethod != "social" && a.AuthMethod != "idc" {
			return nil, provider.Identity{}, fmt.Errorf("Kiro OAuth 需要 access_token、expires_at 和 auth_method（social / idc）")
		}
		if a.AuthMethod == "social" && a.ProfileARN == "" {
			return nil, provider.Identity{}, fmt.Errorf("Kiro social 凭据需要 profile_arn")
		}
		if a.AuthMethod == "idc" && (a.ClientID == "" || a.RefreshToken != "" && a.ClientSecret == "") {
			return nil, provider.Identity{}, fmt.Errorf("Kiro idc 凭据需要 client_id；续期还需要 client_secret")
		}
	}
	for _, s := range []string{a.AccessToken, a.RefreshToken, a.APIKey, a.ClientID, a.ClientSecret, a.ProfileARN} {
		if len(s) > 32768 || strings.ContainsAny(s, "\r\n\x00") {
			return nil, provider.Identity{}, fmt.Errorf("Kiro 凭据字段无效或过长")
		}
	}
	b, e := json.Marshal(a)
	return b, identity(a), e
}
func identity(a Credentials) provider.Identity {
	source := a.ProfileARN
	name := "Kiro OAuth"
	if a.AuthMethod == "api_key" {
		source = a.APIKey
		name = "Kiro API Key"
	} else if source == "" {
		source = a.ClientID
		name = "Kiro Builder ID / IAM"
	}
	digest := sha256.Sum256([]byte(a.AuthMethod + ":" + source))
	return provider.Identity{ID: hex.EncodeToString(digest[:16]), Name: name, Scope: a.Region, ExpiresAt: a.ExpiresAt, HasRefresh: a.RefreshToken != "", NonExpiring: a.AuthMethod == "api_key"}
}
func decode(raw json.RawMessage) (Credentials, error) {
	var a Credentials
	e := json.Unmarshal(raw, &a)
	return a, e
}
func uuid() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", fmt.Errorf("Kiro 随机源不可用")
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}
func failure(status int) error {
	if status < 400 || status > 599 {
		status = 502
	}
	return &provider.Error{Status: status, AccountFault: status == 401, Message: fmt.Sprintf("Kiro 上游请求失败（HTTP %d）", status)}
}
func invalid(message string) error { return &provider.Error{Status: 400, Message: "Kiro：" + message} }
func broken() error {
	return &provider.Error{Status: 502, Message: "Kiro 返回了无效或不完整的响应"}
}
func (c *Client) request(ctx context.Context, method, url string, a Credentials, target string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		reader = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, url, reader)
	if e != nil {
		return nil, broken()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codearts2api-panel/kiro")
	req.Header.Set("X-Amzn-Codewhisperer-Optout", "true")
	if target != "" {
		req.Header.Set("X-Amz-Target", target)
		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	}
	if a.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.APIKey)
		req.Header.Set("TokenType", "API_KEY")
	} else if a.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	}
	resp, e := c.HTTP.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &provider.Error{Status: 502, Message: "Kiro 上游连接失败"}
	}
	return resp, nil
}
func readJSON(resp *http.Response) (map[string]json.RawMessage, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, failure(resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	var o map[string]json.RawMessage
	if e != nil || len(b) > 4<<20 || json.Unmarshal(b, &o) != nil || o == nil {
		return nil, broken()
	}
	return o, nil
}
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	if a.AuthMethod == "api_key" {
		return raw, nil
	}
	if a.RefreshToken == "" {
		return nil, invalid("没有可续期凭据，请重新登录")
	}
	region, ok := c.Regions[a.AuthRegion]
	if !ok {
		return nil, invalid("auth_region 无效")
	}
	url := region.Social + "/refreshToken"
	body := map[string]string{"refreshToken": a.RefreshToken}
	if a.AuthMethod == "idc" {
		if a.ClientSecretExpiresAt > 0 && a.ClientSecretExpiresAt <= time.Now().Unix() {
			return nil, invalid("OIDC 客户端凭据已过期，请重新登录")
		}
		url = region.OIDC + "/token"
		body["clientId"] = a.ClientID
		body["clientSecret"] = a.ClientSecret
		body["grantType"] = "refresh_token"
	}
	resp, e := c.request(ctx, http.MethodPost, url, Credentials{}, "", body)
	if e != nil {
		return nil, e
	}
	o, e := readJSON(resp)
	if e != nil {
		return nil, e
	}
	return applyToken(a, o)
}
func applyToken(a Credentials, o map[string]json.RawMessage) (json.RawMessage, error) {
	token := field(o, "accessToken")
	if a.AuthMethod == "idc" {
		if id := field(o, "idToken"); id != "" {
			token = id
		}
	}
	var seconds int64
	if token == "" || json.Unmarshal(o["expiresIn"], &seconds) != nil || seconds <= 0 || seconds > 365*86400 {
		return nil, broken()
	}
	a.AccessToken = token
	a.ExpiresAt = time.Now().Unix() + seconds
	if rt := field(o, "refreshToken"); rt != "" {
		a.RefreshToken = rt
	}
	// No profile/catalog I/O after a token rotation. The credential coordinator
	// commits this generation before any subsequent fallible operation.
	return json.Marshal(a)
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	a, e := decode(raw)
	if e != nil {
		return provider.Identity{}, e
	}
	m, e := c.Models(ctx, raw)
	if e != nil {
		return provider.Identity{}, e
	}
	if len(m) == 0 {
		return provider.Identity{}, invalid("该凭据没有可用模型")
	}
	return identity(a), nil
}
