// Package claude implements Anthropic Messages with OAuth or API-key credentials.
// It does not execute Claude Code, local tools, or browser cookies.
package claude

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

const clientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
const redirectURI = "https://platform.claude.com/oauth/code/callback"

type Credentials struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	AccountUUID  string `json:"account_uuid,omitempty"`
	Name         string `json:"name,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	AuthMode     string `json:"auth_mode"`
}
type Client struct {
	HTTP       *http.Client
	API, Token string
	Login      *providerhttp.PKCE
}

func New() *Client {
	return &Client{HTTP: providerhttp.New(), API: "https://api.anthropic.com", Token: "https://platform.claude.com/v1/oauth/token", Login: &providerhttp.PKCE{Config: providerhttp.OAuthConfig{Name: "Claude", Authorize: "https://claude.com/cai/oauth/authorize", Redirect: redirectURI, ClientID: clientID, Scopes: "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload", Extra: url.Values{"code": {"true"}}}}}
}
func (*Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.Claude, Name: "Claude / Anthropic", Available: true, Scopes: []string{"global"}, Login: true}
}
func (*Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var o map[string]json.RawMessage
	if json.Unmarshal(raw, &o) != nil || o == nil {
		return nil, provider.Identity{}, providerhttp.Invalid("Claude", "凭据须为 JSON 对象")
	}
	// The desktop export's nested OAuth tokens still require an explicit stable
	// account_uuid. Refresh-token hashes cannot be used as account identities.
	if b := o["claudeAiOauth"]; len(b) > 0 {
		var nested map[string]json.RawMessage
		if json.Unmarshal(b, &nested) != nil || nested == nil {
			return nil, provider.Identity{}, providerhttp.Invalid("Claude", "claudeAiOauth 格式无效")
		}
		for k, v := range nested {
			if _, exists := o[k]; !exists {
				o[k] = v
			}
		}
	}
	a := Credentials{AccessToken: providerhttp.Field(o, "access_token", "accessToken"), RefreshToken: providerhttp.Field(o, "refresh_token", "refreshToken"), APIKey: providerhttp.Field(o, "api_key", "ANTHROPIC_API_KEY"), ExpiresAt: providerhttp.Timestamp(o["expires_at"]), AccountUUID: providerhttp.Field(o, "account_uuid", "accountUuid"), Name: providerhttp.Field(o, "name", "email"), ClientID: providerhttp.Field(o, "client_id")}
	if a.ExpiresAt == 0 {
		a.ExpiresAt = providerhttp.Timestamp(o["expiresAt"])
	}
	if scope := providerhttp.Field(o, "scope"); scope != "" && scope != "global" {
		return nil, provider.Identity{}, providerhttp.Invalid("Claude", "地区须为 global")
	}
	if a.APIKey != "" {
		if a.AccessToken != "" || a.RefreshToken != "" || !strings.HasPrefix(a.APIKey, "sk-ant-") {
			return nil, provider.Identity{}, providerhttp.Invalid("Claude", "API Key 须为 sk-ant- 开头，不能与 OAuth 令牌混用")
		}
		a.AuthMode = "api_key"
		a.ExpiresAt = 0
		a.AccountUUID = ""
		a.ClientID = ""
	} else {
		a.AuthMode = "oauth"
		if a.ClientID == "" {
			a.ClientID = clientID
		}
		if a.AccessToken == "" || a.AccountUUID == "" || a.ExpiresAt <= 0 {
			return nil, provider.Identity{}, providerhttp.Invalid("Claude", "OAuth 需要 access_token、account_uuid 和 expires_at")
		}
	}
	if !providerhttp.Safe(a.AccessToken, a.RefreshToken, a.APIKey, a.AccountUUID, a.Name, a.ClientID) {
		return nil, provider.Identity{}, providerhttp.Invalid("Claude", "凭据字段无效或过长")
	}
	b, e := json.Marshal(a)
	return b, identity(a), e
}
func identity(a Credentials) provider.Identity {
	source, name := a.AccountUUID, a.Name
	if a.AuthMode == "api_key" {
		source = a.APIKey
		if name == "" {
			name = "Anthropic API Key"
		}
	}
	if name == "" {
		name = "Claude"
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
	h.Set("anthropic-version", "2023-06-01")
	if a.AuthMode == "api_key" {
		h.Set("x-api-key", a.APIKey)
	} else {
		h.Set("Authorization", "Bearer "+a.AccessToken)
		h.Set("anthropic-beta", "oauth-2025-04-20")
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
		resp, e := providerhttp.Do(ctx, c.HTTP, "Claude", http.MethodPost, c.Token, nil, map[string]string{"grant_type": "authorization_code", "client_id": c.Login.Config.ClientID, "redirect_uri": c.Login.Config.Redirect, "code": code, "state": state, "code_verifier": verifier})
		if e != nil {
			return nil, e
		}
		o, e := providerhttp.JSON(resp, "Claude")
		if e != nil {
			return nil, e
		}
		return c.applyToken(Credentials{AuthMode: "oauth", ClientID: c.Login.Config.ClientID}, o)
	})
}
func (c *Client) applyToken(a Credentials, o map[string]json.RawMessage) (json.RawMessage, error) {
	token := providerhttp.Field(o, "access_token")
	var seconds int64
	_ = json.Unmarshal(o["expires_in"], &seconds)
	if token == "" || seconds <= 0 || seconds > 365*86400 {
		return nil, providerhttp.Broken("Claude")
	}
	var account map[string]json.RawMessage
	_ = json.Unmarshal(o["account"], &account)
	id := providerhttp.Field(account, "uuid")
	if a.AccountUUID != "" && id != "" && a.AccountUUID != id {
		return nil, providerhttp.Invalid("Claude", "轮转响应的账号身份发生变化")
	}
	a.AccessToken = token
	a.ExpiresAt = time.Now().Unix() + seconds
	if rt := providerhttp.Field(o, "refresh_token"); rt != "" {
		a.RefreshToken = rt
	}
	if a.AccountUUID == "" {
		a.AccountUUID = id
	}
	if a.Name == "" {
		a.Name = providerhttp.Field(account, "email_address")
	}
	b, e := json.Marshal(a)
	if e != nil {
		return nil, e
	}
	b, _, e = c.Normalize(b)
	return b, e
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
		return nil, providerhttp.Invalid("Claude", "没有可续期凭据，请重新登录")
	}
	resp, e := providerhttp.Do(ctx, c.HTTP, "Claude", http.MethodPost, c.Token, nil, map[string]string{"grant_type": "refresh_token", "refresh_token": a.RefreshToken, "client_id": a.ClientID})
	if e != nil {
		return nil, e
	}
	o, e := providerhttp.JSON(resp, "Claude")
	if e != nil {
		return nil, e
	}
	return c.applyToken(a, o)
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		q := url.Values{"limit": {"1000"}}
		if cursor != "" {
			q.Set("after_id", cursor)
		}
		resp, e := providerhttp.Do(ctx, c.HTTP, "Claude", http.MethodGet, c.API+"/v1/models?"+q.Encode(), headers(a), nil)
		if e != nil {
			return nil, e
		}
		o, e := providerhttp.JSON(resp, "Claude")
		if e != nil {
			return nil, e
		}
		var rows []struct {
			ID        string `json:"id"`
			Name      string `json:"display_name"`
			MaxInput  int    `json:"max_input_tokens"`
			MaxOutput int    `json:"max_tokens"`
		}
		if json.Unmarshal(o["data"], &rows) != nil {
			return nil, providerhttp.Broken("Claude")
		}
		for _, r := range rows {
			if !providerhttp.ModelID(r.ID) || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			if r.Name == "" {
				r.Name = r.ID
			}
			if r.MaxInput < 0 || r.MaxOutput < 0 {
				return nil, providerhttp.Broken("Claude")
			}
			out = append(out, provider.Model{ID: r.ID, Name: r.Name, Context: r.MaxInput, MaxOutput: r.MaxOutput, Provider: provider.Claude, Scope: "global", UpstreamID: r.ID, Source: "Claude / Anthropic", StreamOnly: true})
		}
		var more bool
		if b := o["has_more"]; len(b) > 0 && json.Unmarshal(b, &more) != nil {
			return nil, providerhttp.Broken("Claude")
		}
		if !more {
			return out, nil
		}
		next := providerhttp.Field(o, "last_id")
		if len(rows) == 0 || next == "" || next == cursor {
			return nil, providerhttp.Broken("Claude")
		}
		cursor = next
	}
	return nil, providerhttp.Broken("Claude")
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	a, e := decode(raw)
	if e != nil {
		return provider.Identity{}, e
	}
	if a.AuthMode == "oauth" {
		resp, e := providerhttp.Do(ctx, c.HTTP, "Claude", http.MethodGet, c.API+"/api/oauth/profile", headers(a), nil)
		if e != nil {
			return provider.Identity{}, e
		}
		o, e := providerhttp.JSON(resp, "Claude")
		if e != nil {
			return provider.Identity{}, e
		}
		var account map[string]json.RawMessage
		_ = json.Unmarshal(o["account"], &account)
		got := providerhttp.Field(account, "uuid")
		if got == "" {
			got = providerhttp.Field(o, "uuid", "account_uuid")
		}
		if got != a.AccountUUID {
			return provider.Identity{}, providerhttp.Invalid("Claude", "上游账号身份与导入凭据不一致")
		}
	}
	models, e := c.Models(ctx, raw)
	if e == nil && len(models) == 0 {
		e = providerhttp.Invalid("Claude", "该凭据没有返回可用模型")
	}
	return identity(a), e
}

func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	a, e := decode(raw)
	if e != nil {
		return e
	}
	if !providerhttp.ModelID(r.Model) {
		return providerhttp.Invalid("Claude", "型号无效")
	}
	body, e := messagesBody(r)
	if e != nil {
		return e
	}
	h := headers(a)
	h.Set("Accept", "text/event-stream")
	endpoint := c.API + "/v1/messages"
	if a.AuthMode == "oauth" {
		endpoint += "?beta=true"
	}
	resp, e := providerhttp.Do(ctx, c.HTTP, "Claude", http.MethodPost, endpoint, h, body)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("Claude", resp)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return providerhttp.Broken("Claude")
	}
	return messagesStream(ctx, resp.Body, provider.ModelID(provider.Claude, "global", r.Model), emit)
}
