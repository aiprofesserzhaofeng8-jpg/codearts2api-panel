// Package chatgpt implements the Codex subscription Responses transport and the
// official OpenAI API-key transport. It does not execute a CLI or local tools.
package chatgpt

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const clientID = "app_EMoamEEZ73f0CkXaXp7hrann"

// codexProtocolVersion is the minimum catalog contract in the pinned Sub2API reference.
const codexProtocolVersion = "0.144.0"
const redirectURI = "http://localhost:1455/auth/callback"

type Credentials struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Name         string `json:"name,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	AuthMode     string `json:"auth_mode"`
}
type Client struct {
	HTTP                *http.Client
	API, Backend, Token string
	Login               *providerhttp.PKCE
}

func New() *Client {
	return &Client{HTTP: providerhttp.New(), API: "https://api.openai.com/v1", Backend: "https://chatgpt.com/backend-api/codex", Token: "https://auth.openai.com/oauth/token", Login: &providerhttp.PKCE{Config: providerhttp.OAuthConfig{Name: "ChatGPT", Authorize: "https://auth.openai.com/oauth/authorize", Redirect: redirectURI, ClientID: clientID, Scopes: "openid profile email offline_access", Extra: url.Values{"id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}}}}}
}
func (*Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.ChatGPT, Name: "ChatGPT / Codex", Available: true, Scopes: []string{"global"}, Login: true}
}
func claims(token string) map[string]json.RawMessage {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || len(b) > 64<<10 {
		return nil
	}
	var o map[string]json.RawMessage
	_ = json.Unmarshal(b, &o)
	return o
}
func applyClaims(a *Credentials, token string) {
	o := claims(token)
	var auth map[string]json.RawMessage
	_ = json.Unmarshal(o["https://api.openai.com/auth"], &auth)
	if a.AccountID == "" {
		a.AccountID = providerhttp.Field(auth, "chatgpt_account_id")
	}
	if a.UserID == "" {
		a.UserID = providerhttp.Field(auth, "chatgpt_user_id", "user_id")
		if a.UserID == "" {
			a.UserID = providerhttp.Field(o, "sub")
		}
	}
	if a.Name == "" {
		a.Name = providerhttp.Field(o, "email", "name")
	}
	if a.ExpiresAt == 0 {
		a.ExpiresAt = providerhttp.Timestamp(o["exp"])
	}
}
func (*Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var o map[string]json.RawMessage
	if json.Unmarshal(raw, &o) != nil || o == nil {
		return nil, provider.Identity{}, providerhttp.Invalid("ChatGPT", "凭据须为 JSON 对象")
	}
	if b := o["tokens"]; len(b) > 0 {
		var nested map[string]json.RawMessage
		if json.Unmarshal(b, &nested) != nil || nested == nil {
			return nil, provider.Identity{}, providerhttp.Invalid("ChatGPT", "tokens 格式无效")
		}
		for k, v := range nested {
			if _, exists := o[k]; !exists {
				o[k] = v
			}
		}
	}
	a := Credentials{AccessToken: providerhttp.Field(o, "access_token", "accessToken"), RefreshToken: providerhttp.Field(o, "refresh_token", "refreshToken"), APIKey: providerhttp.Field(o, "api_key", "OPENAI_API_KEY"), AccountID: providerhttp.Field(o, "account_id", "chatgpt_account_id"), UserID: providerhttp.Field(o, "user_id"), Name: providerhttp.Field(o, "name", "email"), ClientID: providerhttp.Field(o, "client_id"), ExpiresAt: providerhttp.Timestamp(o["expires_at"])}
	if a.APIKey != "" {
		if a.AccessToken != "" || a.RefreshToken != "" || !strings.HasPrefix(a.APIKey, "sk-") {
			return nil, provider.Identity{}, providerhttp.Invalid("ChatGPT", "API Key 须为 sk- 开头，不能与 OAuth 令牌混用")
		}
		a.AuthMode = "api_key"
		a.ExpiresAt = 0
		a.AccountID = ""
		a.UserID = ""
		a.ClientID = ""
	} else {
		a.AuthMode = "codex"
		applyClaims(&a, a.AccessToken)
		applyClaims(&a, providerhttp.Field(o, "id_token"))
		if a.ClientID == "" {
			a.ClientID = clientID
		}
		if a.AccessToken == "" || a.AccountID == "" || a.UserID == "" || a.ExpiresAt <= 0 {
			return nil, provider.Identity{}, providerhttp.Invalid("ChatGPT", "OAuth 需要 access_token、account_id 和真实 expires_at（JWT 可自动读取）")
		}
	}
	if scope := providerhttp.Field(o, "scope"); scope != "" && scope != "global" {
		return nil, provider.Identity{}, providerhttp.Invalid("ChatGPT", "地区须为 global")
	}
	if !providerhttp.Safe(a.AccessToken, a.RefreshToken, a.APIKey, a.AccountID, a.UserID, a.Name, a.ClientID) {
		return nil, provider.Identity{}, providerhttp.Invalid("ChatGPT", "凭据字段无效或过长")
	}
	b, e := json.Marshal(a)
	return b, identity(a), e
}
func identity(a Credentials) provider.Identity {
	source, name := a.AccountID+":"+a.UserID, a.Name
	if a.AuthMode == "api_key" {
		source = a.APIKey
		if name == "" {
			name = "OpenAI API Key"
		}
	}
	if name == "" {
		name = "ChatGPT / Codex"
	}
	sum := sha256.Sum256([]byte(a.AuthMode + ":" + source))
	return provider.Identity{ID: hex.EncodeToString(sum[:16]), Name: name, Scope: "global", ExpiresAt: a.ExpiresAt, HasRefresh: a.RefreshToken != "", NonExpiring: a.AuthMode == "api_key"}
}
func decode(raw json.RawMessage) (Credentials, error) {
	var a Credentials
	e := json.Unmarshal(raw, &a)
	return a, e
}
func headers(a Credentials) http.Header {
	h := http.Header{}
	token := a.AccessToken
	if a.APIKey != "" {
		token = a.APIKey
	}
	h.Set("Authorization", "Bearer "+token)
	if a.AuthMode == "codex" {
		h.Set("ChatGPT-Account-Id", a.AccountID)
		h.Set("OpenAI-Beta", "responses=experimental")
	}
	return h
}
func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	return c.Login.Begin(ctx, scope)
}
func (c *Client) ReleaseLogin(state string) { c.Login.Release(state) }
func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	return c.CheckCallback(ctx, scope, state, "")
}
func (c *Client) CheckCallback(ctx context.Context, scope, state, input string) (json.RawMessage, bool, error) {
	return c.Login.Check(ctx, scope, state, input, func(ctx context.Context, code, verifier string) (json.RawMessage, error) {
		resp, e := providerhttp.Do(ctx, c.HTTP, "ChatGPT", http.MethodPost, c.Token, http.Header{}, map[string]string{"grant_type": "authorization_code", "client_id": c.Login.Config.ClientID, "redirect_uri": c.Login.Config.Redirect, "code": code, "code_verifier": verifier})
		if e != nil {
			return nil, e
		}
		o, e := providerhttp.JSON(resp, "ChatGPT")
		if e != nil {
			return nil, e
		}
		return c.applyToken(Credentials{AuthMode: "codex", ClientID: c.Login.Config.ClientID}, o)
	})
}
func (c *Client) applyToken(a Credentials, o map[string]json.RawMessage) (json.RawMessage, error) {
	token := providerhttp.Field(o, "access_token")
	var seconds int64
	_ = json.Unmarshal(o["expires_in"], &seconds)
	if token == "" || seconds <= 0 || seconds > 365*86400 {
		return nil, providerhttp.Broken("ChatGPT")
	}
	next := a
	next.AccessToken = token
	next.ExpiresAt = time.Now().Unix() + seconds
	if rt := providerhttp.Field(o, "refresh_token"); rt != "" {
		next.RefreshToken = rt
	}
	// JWT fields are routing metadata, not proof of identity or gateway permissions.
	probe := Credentials{}
	applyClaims(&probe, token)
	applyClaims(&probe, providerhttp.Field(o, "id_token"))
	if a.AccountID != "" && probe.AccountID != "" && probe.AccountID != a.AccountID || a.UserID != "" && probe.UserID != "" && probe.UserID != a.UserID {
		return nil, providerhttp.Invalid("ChatGPT", "轮转响应的账号身份发生变化")
	}
	if next.AccountID == "" {
		next.AccountID = probe.AccountID
	}
	if next.UserID == "" && a.AccountID == "" {
		next.UserID = probe.UserID
	}
	if next.Name == "" {
		next.Name = probe.Name
	}
	b, e := json.Marshal(next)
	if e != nil {
		return nil, e
	}
	normalized, _, e := c.Normalize(b)
	return normalized, e
}
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	if a.AuthMode == "api_key" {
		return raw, nil
	}
	if a.RefreshToken == "" {
		return nil, providerhttp.Invalid("ChatGPT", "没有可续期凭据，请重新登录")
	}
	resp, e := providerhttp.Do(ctx, c.HTTP, "ChatGPT", http.MethodPost, c.Token, http.Header{}, map[string]string{"grant_type": "refresh_token", "client_id": a.ClientID, "refresh_token": a.RefreshToken})
	if e != nil {
		return nil, e
	}
	o, e := providerhttp.JSON(resp, "ChatGPT")
	if e != nil {
		return nil, e
	}
	// No profile/catalog I/O after rotation; the existing coordinator commits next.
	return c.applyToken(a, o)
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	endpoint := c.API + "/models"
	if a.AuthMode == "codex" {
		endpoint = c.Backend + "/models?client_version=" + codexProtocolVersion
	}
	resp, e := providerhttp.Do(ctx, c.HTTP, "ChatGPT", http.MethodGet, endpoint, headers(a), nil)
	if e != nil {
		return nil, e
	}
	o, e := providerhttp.JSON(resp, "ChatGPT")
	if e != nil {
		return nil, e
	}
	var rows []struct {
		ID        string `json:"id"`
		Slug      string `json:"slug"`
		Name      string `json:"display_name"`
		Context   int    `json:"context_window"`
		MaxOutput int    `json:"max_output_tokens"`
		Hidden    bool   `json:"hide_in_picker"`
	}
	key := "data"
	if a.AuthMode == "codex" {
		key = "models"
	}
	if json.Unmarshal(o[key], &rows) != nil {
		return nil, providerhttp.Broken("ChatGPT")
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	for _, r := range rows {
		id := r.ID
		if id == "" {
			id = r.Slug
		}
		if !providerhttp.ModelID(id) || seen[id] {
			continue
		}
		seen[id] = true
		if r.Name == "" {
			r.Name = id
		}
		if r.Context < 0 || r.MaxOutput < 0 {
			return nil, providerhttp.Broken("ChatGPT")
		}
		out = append(out, provider.Model{ID: id, Name: r.Name, Context: r.Context, MaxOutput: r.MaxOutput, Provider: provider.ChatGPT, Scope: "global", UpstreamID: id, Source: "ChatGPT / OpenAI", StreamOnly: true})
	}
	return out, nil
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	a, e := decode(raw)
	if e != nil {
		return provider.Identity{}, e
	}
	models, e := c.Models(ctx, raw)
	if e == nil && len(models) == 0 {
		e = providerhttp.Invalid("ChatGPT", "该凭据没有返回可用模型")
	}
	return identity(a), e
}
func (*Client) Credit(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"available":false,"reason":"ChatGPT / OpenAI 配额以官方账号页面为准"}`), nil
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	a, e := decode(raw)
	if e != nil {
		return e
	}
	if !providerhttp.ModelID(r.Model) {
		return providerhttp.Invalid("ChatGPT", "型号无效")
	}
	body, e := responsesBody(r, a.AuthMode == "codex")
	if e != nil {
		return e
	}
	endpoint := c.API + "/responses"
	if a.AuthMode == "codex" {
		endpoint = c.Backend + "/responses"
	}
	h := headers(a)
	h.Set("Accept", "text/event-stream")
	if r.SessionID != "" {
		h.Set("Session_id", r.SessionID)
	}
	resp, e := providerhttp.Do(ctx, c.HTTP, "ChatGPT", http.MethodPost, endpoint, h, body)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.Failure("ChatGPT", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return providerhttp.Broken("ChatGPT")
	}
	return responsesStream(ctx, resp.Body, provider.ModelID(provider.ChatGPT, "global", r.Model), emit)
}
