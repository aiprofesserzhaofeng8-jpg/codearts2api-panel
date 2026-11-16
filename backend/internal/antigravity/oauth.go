// SPDX-License-Identifier: LGPL-3.0-only
// Adapted Sub2API protocol mappings; see UPSTREAM.md.
package antigravity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type credentials struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token,omitempty"`
	Subject string `json:"subject"`
	Project string `json:"project_id"`
	Name    string `json:"name,omitempty"`
	Expires int64  `json:"expires_at"`
	Scope   string `json:"scope"`
	Auth    string `json:"auth_mode"`
}

func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var a credentials
	var o map[string]json.RawMessage
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(raw, &o) != nil {
		return nil, provider.Identity{}, providerhttp.Invalid("Antigravity", "凭据格式无效")
	}
	if a.Access == "" || a.Subject == "" || a.Project == "" || a.Expires <= 0 || a.Scope != "global" || (a.Auth != "" && a.Auth != "oauth") || !providerhttp.Safe(a.Access, a.Refresh, a.Subject, a.Project, a.Name) {
		return nil, provider.Identity{}, providerhttp.Invalid("Antigravity", "需要 access_token、subject、project_id、expires_at 和 scope=global")
	}
	for _, k := range []string{"api_key", "base_url", "api_base_urls", "client_id", "client_secret"} {
		if b := o[k]; len(b) > 0 && string(b) != "null" && string(b) != `""` {
			return nil, provider.Identity{}, providerhttp.Invalid("Antigravity", "不接受混用密钥、自定义端点或 OAuth 客户端")
		}
	}
	a.Auth = "oauth"
	a.Scope = "global"
	sum := sha256.Sum256([]byte("antigravity:global:" + a.Subject + ":" + a.Project))
	name := a.Name
	if name == "" {
		name = "Antigravity"
	}
	b, e := json.Marshal(a)
	return b, provider.Identity{ID: hex.EncodeToString(sum[:16]), Name: name, Scope: "global", ExpiresAt: a.Expires, HasRefresh: a.Refresh != ""}, e
}
func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	if err := c.oauthConfigured(); err != nil {
		return provider.Login{}, err
	}
	return c.Login.Begin(ctx, scope)
}
func (c *Client) ReleaseLogin(state string) { c.Login.Release(state) }
func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	return c.CheckCallback(ctx, scope, state, "")
}
func (c *Client) CheckCallback(ctx context.Context, scope, state, input string) (json.RawMessage, bool, error) {
	raw, ready, err := c.Login.Check(ctx, scope, state, input, func(ctx context.Context, code, verifier string) (json.RawMessage, error) {
		token, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {RedirectURI}})
		if err != nil {
			return nil, err
		}
		return json.Marshal(credentials{Access: token.AccessToken, Refresh: token.RefreshToken, Expires: time.Now().Unix() + token.ExpiresIn, Scope: "global", Auth: "oauth"})
	})
	if err != nil || !ready {
		return raw, ready, err
	}
	// Token redemption is cached by PKCE before project/user validation. A retry
	// never redeems the one-time code again and never writes an account here.
	var a credentials
	_ = json.Unmarshal(raw, &a)
	if a.Expires <= time.Now().Unix() {
		return nil, false, providerhttp.Invalid("Antigravity", "授权令牌已过期，请重新授权")
	}
	if err = c.prepare(ctx, &a, true); err != nil {
		return nil, false, err
	}
	b, err := json.Marshal(a)
	return b, err == nil, err
}
func (c *Client) token(ctx context.Context, values url.Values) (TokenResponse, error) {
	if err := c.oauthConfigured(); err != nil {
		return TokenResponse{}, err
	}
	values.Set("client_id", c.Login.Config.ClientID)
	values.Set("client_secret", c.clientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return TokenResponse{}, providerhttp.Broken("Antigravity")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return TokenResponse{}, ctx.Err()
		}
		return TokenResponse{}, provider.NewTransportError("Antigravity", err)
	}
	obj, err := providerhttp.JSON(resp, "Antigravity")
	if err != nil {
		return TokenResponse{}, err
	}
	b, _ := json.Marshal(obj)
	var tok TokenResponse
	if json.Unmarshal(b, &tok) != nil || tok.AccessToken == "" || tok.ExpiresIn <= 0 || tok.ExpiresIn > 86400 || !strings.EqualFold(tok.TokenType, "Bearer") || !providerhttp.Safe(tok.AccessToken, tok.RefreshToken) {
		return tok, providerhttp.Broken("Antigravity")
	}
	return tok, nil
}
func (c *Client) cliCall(ctx context.Context, a credentials, method, path string, body any) (map[string]json.RawMessage, error) {
	resp, err := providerhttp.Do(ctx, c.HTTP, "Antigravity", method, c.BaseURL+path, http.Header{"Authorization": {"Bearer " + a.Access}, "User-Agent": {UserAgent}}, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, providerhttp.FailureResponse("Antigravity", resp)
	}
	return providerhttp.JSON(resp, "Antigravity")
}
func projectID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var o map[string]json.RawMessage
	_ = json.Unmarshal(raw, &o)
	return providerhttp.Field(o, "id")
}
func (c *Client) prepare(ctx context.Context, a *credentials, allowOnboard bool) error {
	resp, err := providerhttp.Do(ctx, c.HTTP, "Antigravity", http.MethodGet, c.UserInfoURL, http.Header{"Authorization": {"Bearer " + a.Access}, "User-Agent": {UserAgent}}, nil)
	if err != nil {
		return err
	}
	user, err := providerhttp.JSON(resp, "Antigravity")
	if err != nil {
		return err
	}
	sub := providerhttp.Field(user, "id", "sub")
	if sub == "" || (a.Subject != "" && sub != a.Subject) {
		return providerhttp.Invalid("Antigravity", "Google 身份与原账号不匹配")
	}
	a.Subject = sub
	a.Name = providerhttp.Field(user, "email", "name")
	metadata := map[string]string{"ideType": "ANTIGRAVITY", "ideVersion": UserAgentVersion, "ideName": "antigravity"}
	payload := map[string]any{"metadata": metadata}
	obj, err := c.cliCall(ctx, *a, "POST", "/v1internal:loadCodeAssist", payload)
	if err != nil {
		return err
	}
	if p := projectID(obj["cloudaicompanionProject"]); p != "" {
		if a.Project != "" && a.Project != p {
			return providerhttp.Invalid("Antigravity", "项目与原账号不匹配")
		}
		a.Project = p
		return nil
	}
	if !allowOnboard {
		return providerhttp.Invalid("Antigravity", "未返回项目，请通过浏览器授权完成初始化")
	}
	return c.onboard(ctx, a, obj)
}

func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	b, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	err = c.prepare(ctx, &a, false)
	return id, err
}
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	if a.Refresh == "" {
		return nil, providerhttp.Invalid("Antigravity", "缺少 refresh_token，请重新授权")
	}
	tok, err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {a.Refresh}})
	if err != nil {
		return nil, err
	}
	a.Access = tok.AccessToken
	a.Expires = time.Now().Unix() + tok.ExpiresIn
	if tok.RefreshToken != "" {
		a.Refresh = tok.RefreshToken
	}
	// Do not add optional network checks after rotating credentials. Existing CAS
	// coordinator persists them before another request borrows the new generation.
	return json.Marshal(a)
}

func (c *Client) oauthConfigured() error {
	if c.Login.Config.ClientID == "" || c.clientSecret == "" || !providerhttp.Safe(c.Login.Config.ClientID, c.clientSecret) {
		return providerhttp.Invalid("antigravity", "服务端未配置 OAuth：请设置 ANTIGRAVITY_OAUTH_CLIENT_ID 和 ANTIGRAVITY_OAUTH_CLIENT_SECRET 后重启服务")
	}
	return nil
}
