// SPDX-License-Identifier: LGPL-3.0-only
// Adapted Sub2API protocol mappings; see UPSTREAM.md.
package gemini

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

type cliCredentials struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token,omitempty"`
	Subject string `json:"subject"`
	Project string `json:"project_id"`
	Name    string `json:"name,omitempty"`
	Expires int64  `json:"expires_at"`
	Scope   string `json:"scope"`
	Auth    string `json:"auth_mode"`
}

func isCLI(raw json.RawMessage) bool {
	var o map[string]json.RawMessage
	if json.Unmarshal(raw, &o) != nil {
		return false
	}
	return providerhttp.Field(o, "scope") == "cli" || providerhttp.Field(o, "access_token") != "" || providerhttp.Field(o, "refresh_token") != ""
}
func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "gemini", Name: "Gemini", Scopes: []string{"api", "cli"}, Login: true, Available: true}
}
func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	if !isCLI(raw) {
		return c.Client.Normalize(raw)
	}
	var a cliCredentials
	var o map[string]json.RawMessage
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(raw, &o) != nil {
		return nil, provider.Identity{}, providerhttp.Invalid("Gemini", "CLI 凭据格式无效")
	}
	if a.Access == "" || a.Subject == "" || a.Project == "" || a.Expires <= 0 || a.Scope != "cli" || (a.Auth != "" && a.Auth != "oauth") || !providerhttp.Safe(a.Access, a.Refresh, a.Subject, a.Project, a.Name) {
		return nil, provider.Identity{}, providerhttp.Invalid("Gemini", "CLI 需要 access_token、subject、project_id、expires_at 和 scope=cli")
	}
	for _, k := range []string{"api_key", "base_url", "api_base_urls", "client_id", "client_secret"} {
		if b := o[k]; len(b) > 0 && string(b) != "null" && string(b) != `""` {
			return nil, provider.Identity{}, providerhttp.Invalid("Gemini", "CLI 不接受混用密钥、自定义端点或 OAuth 客户端")
		}
	}
	a.Auth = "oauth"
	a.Scope = "cli"
	sum := sha256.Sum256([]byte("gemini:cli:" + a.Subject + ":" + a.Project))
	name := a.Name
	if name == "" {
		name = "Gemini CLI"
	}
	b, e := json.Marshal(a)
	return b, provider.Identity{ID: hex.EncodeToString(sum[:16]), Name: name, Scope: "cli", ExpiresAt: a.Expires, HasRefresh: a.Refresh != ""}, e
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
		token, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {GeminiCLIRedirectURI}})
		if err != nil {
			return nil, err
		}
		return json.Marshal(cliCredentials{Access: token.AccessToken, Refresh: token.RefreshToken, Expires: time.Now().Unix() + token.ExpiresIn, Scope: "cli", Auth: "oauth"})
	})
	if err != nil || !ready {
		return raw, ready, err
	}
	// Token redemption is cached by PKCE before project/user validation. A retry
	// never redeems the one-time code again and never writes an account here.
	var a cliCredentials
	_ = json.Unmarshal(raw, &a)
	if a.Expires <= time.Now().Unix() {
		return nil, false, providerhttp.Invalid("Gemini", "授权令牌已过期，请重新授权")
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
		return TokenResponse{}, providerhttp.Broken("Gemini")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return TokenResponse{}, ctx.Err()
		}
		return TokenResponse{}, provider.NewTransportError("Gemini", err)
	}
	obj, err := providerhttp.JSON(resp, "Gemini")
	if err != nil {
		return TokenResponse{}, err
	}
	b, _ := json.Marshal(obj)
	var tok TokenResponse
	if json.Unmarshal(b, &tok) != nil || tok.AccessToken == "" || tok.ExpiresIn <= 0 || tok.ExpiresIn > 86400 || !strings.EqualFold(tok.TokenType, "Bearer") || !providerhttp.Safe(tok.AccessToken, tok.RefreshToken) {
		return tok, providerhttp.Broken("Gemini")
	}
	return tok, nil
}
func (c *Client) cliCall(ctx context.Context, a cliCredentials, method, path string, body any) (map[string]json.RawMessage, error) {
	resp, err := providerhttp.Do(ctx, c.HTTP, "Gemini", method, c.CLIBase+path, http.Header{"Authorization": {"Bearer " + a.Access}, "User-Agent": {GeminiCLIUserAgent}}, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, providerhttp.FailureResponse("Gemini", resp)
	}
	return providerhttp.JSON(resp, "Gemini")
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
func (c *Client) prepare(ctx context.Context, a *cliCredentials, onboard bool) error {
	resp, err := providerhttp.Do(ctx, c.HTTP, "Gemini", http.MethodGet, c.UserInfoURL, http.Header{"Authorization": {"Bearer " + a.Access}, "User-Agent": {GeminiCLIUserAgent}}, nil)
	if err != nil {
		return err
	}
	user, err := providerhttp.JSON(resp, "Gemini")
	if err != nil {
		return err
	}
	sub := providerhttp.Field(user, "id", "sub")
	if sub == "" || (a.Subject != "" && sub != a.Subject) {
		return providerhttp.Invalid("Gemini", "Google 身份与原账号不匹配")
	}
	a.Subject = sub
	a.Name = providerhttp.Field(user, "email", "name")
	metadata := LoadCodeAssistMetadata{IDEType: "GEMINI_CLI", Platform: "PLATFORM_UNSPECIFIED", PluginType: "GEMINI"}
	payload := map[string]any{"metadata": metadata}
	if a.Project != "" {
		payload["cloudaicompanionProject"] = a.Project
	}
	obj, err := c.cliCall(ctx, *a, "POST", "/v1internal:loadCodeAssist", payload)
	if err != nil {
		return err
	}
	if p := projectID(obj["cloudaicompanionProject"]); p != "" {
		if a.Project != "" && a.Project != p {
			return providerhttp.Invalid("Gemini", "项目与原账号不匹配")
		}
		a.Project = p
		return nil
	}
	var tier TierInfo
	_ = json.Unmarshal(obj["paidTier"], &tier)
	if tier.ID == "" {
		_ = json.Unmarshal(obj["currentTier"], &tier)
	}
	if tier.ID != "" {
		if a.Project != "" {
			return nil
		}
		return providerhttp.Invalid("Gemini", "已注册但未返回项目，请从官方 CLI 导入包含 project_id 的完整凭据")
	}
	if !onboard {
		return providerhttp.Invalid("Gemini", "Code Assist 未初始化，请先浏览器授权")
	}
	var tiers []AllowedTier
	_ = json.Unmarshal(obj["allowedTiers"], &tiers)
	chosen := ""
	for _, t := range tiers {
		if t.IsDefault {
			chosen = t.ID
			break
		}
	}
	if chosen == "" {
		return providerhttp.Invalid("Gemini", "未提供默认 Code Assist 套餐，未代选付费套餐")
	}
	// One onboarding request, then read-only operation polling; no repeated writes.
	obj, err = c.cliCall(ctx, *a, "POST", "/v1internal:onboardUser", OnboardUserRequest{TierID: chosen, Metadata: metadata})
	if err != nil {
		return err
	}
	for attempt := 0; attempt <= 5; attempt++ {
		if b := obj["error"]; len(b) > 0 && string(b) != "null" {
			return providerhttp.Broken("Gemini")
		}
		var done bool
		_ = json.Unmarshal(obj["done"], &done)
		if done {
			var result map[string]json.RawMessage
			_ = json.Unmarshal(obj["response"], &result)
			a.Project = projectID(result["cloudaicompanionProject"])
			if a.Project == "" {
				return providerhttp.Broken("Gemini")
			}
			return nil
		}
		if attempt == 5 {
			break
		}
		name := providerhttp.Field(obj, "name")
		if !strings.HasPrefix(name, "operations/") || strings.ContainsAny(name, "?#\\") || strings.Contains(name, "..") {
			return providerhttp.Broken("Gemini")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		obj, err = c.cliCall(ctx, *a, "GET", "/v1internal/"+name, nil)
		if err != nil {
			return err
		}
	}
	return &provider.Error{Status: 503, Message: "Gemini 项目仍在初始化，请稍后重新检查"}
}
func (c *Client) verifyCLI(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	b, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	var a cliCredentials
	_ = json.Unmarshal(b, &a)
	err = c.prepare(ctx, &a, false)
	return id, err
}
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if !isCLI(raw) {
		return c.Client.Refresh(ctx, raw)
	}
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a cliCredentials
	_ = json.Unmarshal(b, &a)
	if a.Refresh == "" {
		return nil, providerhttp.Invalid("Gemini", "缺少 refresh_token，请重新授权")
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
func (c *Client) cliModels(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	if _, err := c.verifyCLI(ctx, raw); err != nil {
		return nil, err
	}
	out := []provider.Model{}
	for _, m := range DefaultModels {
		if strings.Contains(m.ID, "image") {
			continue
		}
		out = append(out, provider.Model{ID: m.ID, Name: m.DisplayName, Desc: "Code Assist 参考型号；非上游实时权限清单", UpstreamID: m.ID, Provider: "gemini", Scope: "cli", StreamOnly: true, Source: "Sub2API pinned Code Assist reference catalog (not live entitlement discovery)"})
	}
	return out, nil
}

func (c *Client) oauthConfigured() error {
	if c.Login.Config.ClientID == "" || c.clientSecret == "" || !providerhttp.Safe(c.Login.Config.ClientID, c.clientSecret) {
		return providerhttp.Invalid("gemini", "服务端未配置 OAuth：请设置 GEMINI_CLI_OAUTH_CLIENT_ID 和 GEMINI_CLI_OAUTH_CLIENT_SECRET 后重启服务")
	}
	return nil
}
