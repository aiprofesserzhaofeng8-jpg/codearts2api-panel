// SPDX-License-Identifier: LGPL-3.0-only
// xAI OAuth constants and exchange protocol adapted from Sub2API; see UPSTREAM.md.
package grok

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ClientID     = "b1a00492-073a-47ea-816f-4c329264a828"
	AuthorizeURL = "https://auth.x.ai/oauth2/authorize"
	TokenURL     = "https://auth.x.ai/oauth2/token"
	RedirectURI  = "http://127.0.0.1:56121/callback"
	BuildBaseURL = "https://cli-chat-proxy.grok.com/v1"
	OAuthScopes  = "openid profile email offline_access grok-cli:access api:access"
)

// OAuth and transport client. Account authorization is verified upstream
// before preview/import; existing account services own persistence.
type Client struct {
	HTTP              *http.Client
	TokenURL, BaseURL string
	Login             *providerhttp.PKCE
}

func New() *Client {
	return &Client{HTTP: providerhttp.New(), TokenURL: TokenURL, BaseURL: BuildBaseURL, Login: &providerhttp.PKCE{Config: providerhttp.OAuthConfig{Name: "Grok", Authorize: AuthorizeURL, Redirect: RedirectURI, ClientID: ClientID, Scopes: OAuthScopes, AccountScope: "build", Extra: url.Values{"plan": {"generic"}, "referrer": {"codearts2api"}}}}}
}

type credentials struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token,omitempty"`
	Subject string `json:"subject"`
	Team    string `json:"team_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Expires int64  `json:"expires_at"`
	Scope   string `json:"scope"`
	Auth    string `json:"auth_mode"`
}

// Claims are untrusted routing metadata, not local authorization. Parsing does
// not validate a JWT signature, grant account access, or prove a subscription.
func accessClaims(token string) (map[string]json.RawMessage, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 32768 {
		return nil, providerhttp.Invalid("Grok", "需要完整的 Build OAuth access_token")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, providerhttp.Invalid("Grok", "访问令牌格式无效")
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil || obj == nil || providerhttp.Field(obj, "sub") == "" {
		return nil, providerhttp.Invalid("Grok", "访问令牌缺少账号标识")
	}
	return obj, nil
}
func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var a credentials
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(raw, &obj) != nil || a.Scope != "build" || (a.Auth != "" && a.Auth != "oauth") || a.Expires <= 0 || !providerhttp.Safe(a.Access, a.Refresh, a.Subject, a.Team, a.Name) {
		return nil, provider.Identity{}, providerhttp.Invalid("Grok", "Build OAuth 凭据格式无效")
	}
	for _, k := range []string{"api_key", "base_url", "client_id", "client_secret", "sso", "sso_token", "password"} {
		if v := obj[k]; len(v) > 0 && string(v) != "null" && string(v) != `""` {
			return nil, provider.Identity{}, providerhttp.Invalid("Grok", "不接受混合密钥、网页登录材料或自定义端点")
		}
	}
	claims, err := accessClaims(a.Access)
	if err != nil {
		return nil, provider.Identity{}, err
	}
	sub, team := providerhttp.Field(claims, "sub"), providerhttp.Field(claims, "team_id")
	if !providerhttp.Safe(sub, team) || (a.Subject != "" && a.Subject != sub) || (a.Team != "" && a.Team != team) {
		return nil, provider.Identity{}, providerhttp.Invalid("Grok", "令牌身份与凭据不匹配")
	}
	a.Subject = sub
	a.Team = team
	a.Auth = "oauth"
	// Use the earlier of the supplied expiry and the token's own expiry.
	var expiry int64
	if json.Unmarshal(claims["exp"], &expiry) != nil || expiry <= 0 {
		return nil, provider.Identity{}, providerhttp.Invalid("Grok", "访问令牌缺少有效到期时间")
	}
	if expiry < a.Expires {
		a.Expires = expiry
	}
	name := a.Name
	if name == "" {
		name = "Grok Build"
	}
	hash := sha256.Sum256([]byte("grok:build:" + sub + ":" + team))
	b, err := json.Marshal(a)
	return b, provider.Identity{ID: hex.EncodeToString(hash[:16]), Name: name, Scope: "build", ExpiresAt: a.Expires, HasRefresh: a.Refresh != ""}, err
}
func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	login, err := c.Login.Begin(ctx, scope)
	if err != nil {
		return login, err
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		c.Login.Release(login.State)
		return provider.Login{}, err
	}
	u, err := url.Parse(login.URL)
	if err != nil {
		c.Login.Release(login.State)
		return provider.Login{}, err
	}
	q := u.Query()
	q.Set("nonce", base64.RawURLEncoding.EncodeToString(nonce[:]))
	u.RawQuery = q.Encode()
	login.URL = u.String()
	return login, nil
}
func (c *Client) ReleaseLogin(state string) { c.Login.Release(state) }
func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	return c.CheckCallback(ctx, scope, state, "")
}
func (c *Client) CheckCallback(ctx context.Context, scope, state, input string) (json.RawMessage, bool, error) {
	raw, ready, err := c.Login.Check(ctx, scope, state, input, func(ctx context.Context, code, verifier string) (json.RawMessage, error) {
		return c.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {RedirectURI}}, nil)
	})
	if err != nil || !ready {
		return raw, ready, err
	}
	if _, err = c.Verify(ctx, raw); err != nil {
		return nil, false, err
	}
	return raw, true, nil
}
func (c *Client) exchange(ctx context.Context, form url.Values, previous *credentials) (json.RawMessage, error) {
	form.Set("client_id", ClientID)
	req, err := http.NewRequestWithContext(ctx, "POST", c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, providerhttp.Broken("Grok")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "codearts2api-grok-oauth/1.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, provider.NewTransportError("Grok", err)
	}
	obj, err := providerhttp.JSON(resp, "Grok")
	if err != nil {
		return nil, err
	}
	access := providerhttp.Field(obj, "access_token")
	var expires int64
	if json.Unmarshal(obj["expires_in"], &expires) != nil || expires <= 0 || expires > 86400 || !strings.EqualFold(providerhttp.Field(obj, "token_type"), "Bearer") {
		return nil, providerhttp.Broken("Grok")
	}
	a := credentials{Scope: "build", Auth: "oauth"}
	if previous != nil {
		a = *previous
	}
	a.Access = access
	a.Expires = time.Now().Unix() + expires
	if refresh := providerhttp.Field(obj, "refresh_token"); refresh != "" {
		a.Refresh = refresh
	}
	raw, _ := json.Marshal(a)
	normalized, _, err := c.Normalize(raw)
	return normalized, err
}
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(normalized, &a)
	if a.Refresh == "" {
		return nil, providerhttp.Invalid("Grok", "缺少 refresh_token，请重新授权")
	}
	return c.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {a.Refresh}}, &a)
}
