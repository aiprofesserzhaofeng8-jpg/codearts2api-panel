// Package cnapi supplies the shared, stateless Chat Completions transport for
// official API key adapters. It owns no accounts, retries or storage.
package cnapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"codearts2api/internal/chatwire"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type Client struct {
	HTTP                       *http.Client
	ID, Name, Base, BalanceURL string
	// Profiles have fixed scopes and service URLs; credentials only select a declared mode.
	Mode     string
	Coding   *Client
	Scope    string
	Fallback []provider.Model
}

func New(id, name, base, balance string, fallback []provider.Model) *Client {
	return &Client{HTTP: providerhttp.New(), ID: id, Name: name, Base: base, BalanceURL: balance, Fallback: fallback}
}
func (c *Client) Describe() provider.Descriptor {
	scopes := []string{c.scope()}
	if c.Coding != nil {
		scopes = append(scopes, c.Coding.scope())
	}
	return provider.Descriptor{ID: c.ID, Name: c.Name, Available: true, Scopes: scopes}
}

func (c *Client) scope() string {
	if c.Scope != "" {
		return c.Scope
	}
	return "cn"
}

// profile selects a fixed service before credential validation. It never falls
// back to pay-as-you-go after an upstream failure and owns no mutable state.
func (c *Client) profile(raw json.RawMessage) *Client {
	var a credentials
	if c.Coding != nil && json.Unmarshal(raw, &a) == nil && (a.Mode == "coding" || a.Scope == c.Coding.scope()) {
		return c.Coding
	}
	return c
}
func (c *Client) mode() string {
	if c.Mode != "" {
		return c.Mode
	}
	return "payg"
}

type credentials struct {
	Key   string `json:"api_key"`
	Name  string `json:"name,omitempty"`
	Scope string `json:"scope"`
	Mode  string `json:"account_mode"`
	Auth  string `json:"auth_mode"`
}

func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	if p := c.profile(raw); p != c {
		return p.Normalize(raw)
	}
	var fields map[string]json.RawMessage
	var a credentials
	if json.Unmarshal(raw, &fields) != nil || fields == nil || json.Unmarshal(raw, &a) != nil {
		return nil, provider.Identity{}, providerhttp.Invalid(c.Name, "需要 API Key JSON")
	}
	for _, key := range []string{"access_token", "refresh_token", "base_url", "api_base_urls", "zhipu_organization", "zhipu_project"} {
		if v := fields[key]; len(v) > 0 && string(v) != "null" && string(v) != `""` {
			return nil, provider.Identity{}, providerhttp.Invalid(c.Name, "只支持官方 API Key，不支持自定义端点或 OAuth")
		}
	}
	if a.Key == "" || strings.TrimSpace(a.Key) != a.Key || !providerhttp.Safe(a.Key, a.Name) || (a.Scope != "" && a.Scope != c.scope()) || (a.Mode != "" && a.Mode != c.mode()) || (a.Auth != "" && a.Auth != "api_key") {
		return nil, provider.Identity{}, providerhttp.Invalid(c.Name, "API Key 或接入模式无效；仅支持该平台固定范围和所选套餐 / api_key")
	}
	a.Scope = c.scope()
	a.Mode = c.mode()
	a.Auth = "api_key"
	identity := c.ID + ":" + a.Key
	if c.mode() != "payg" {
		identity = c.ID + ":" + c.scope() + ":" + a.Key
	}
	sum := sha256.Sum256([]byte(identity))
	name := a.Name
	if name == "" {
		name = c.Name + " API Key"
	}
	out, _ := json.Marshal(a)
	return out, provider.Identity{ID: hex.EncodeToString(sum[:16]), Name: name, Scope: c.scope(), NonExpiring: true}, nil
}
func (c *Client) Refresh(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, _, e := c.Normalize(raw)
	return b, e
}
func (c *Client) request(ctx context.Context, raw json.RawMessage, method, url string, body any) (*http.Response, error) {
	b, _, e := c.Normalize(raw)
	if e != nil {
		return nil, e
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	return providerhttp.Do(ctx, c.HTTP, c.Name, method, url, http.Header{"Authorization": {"Bearer " + a.Key}, "Accept": {"application/json, text/event-stream"}}, body)
}
func (c *Client) models(ctx context.Context, raw json.RawMessage) ([]provider.Model, bool, error) {
	resp, e := c.request(ctx, raw, "GET", c.Base+"/models", nil)
	if e != nil {
		return nil, false, e
	}
	defer resp.Body.Close()
	if (resp.StatusCode == 404 || resp.StatusCode == 405) && len(c.Fallback) > 0 {
		out := append([]provider.Model(nil), c.Fallback...)
		for i := range out {
			c.decorate(&out[i])
		}
		return out, true, nil
	}
	o, e := providerhttp.JSON(resp, c.Name)
	if e != nil {
		return nil, false, e
	}
	var rows []struct {
		ID      string `json:"id"`
		Context int    `json:"context_window"`
		Output  int    `json:"max_output_tokens"`
	}
	if json.Unmarshal(o["data"], &rows) != nil || rows == nil {
		return nil, false, providerhttp.Broken(c.Name)
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	for _, r := range rows {
		if !providerhttp.ModelID(r.ID) || r.Context < 0 || r.Output < 0 {
			return nil, false, providerhttp.Broken(c.Name)
		}
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		m := provider.Model{ID: r.ID, Name: r.ID, Context: r.Context, MaxOutput: r.Output}
		c.decorate(&m)
		out = append(out, m)
	}
	return out, false, nil
}
func (c *Client) decorate(m *provider.Model) {
	m.Provider = c.ID
	m.Scope = c.scope()
	m.UpstreamID = m.ID
	m.StreamOnly = true
	for _, known := range c.Fallback {
		if known.ID == m.ID {
			m.ImageInput = known.ImageInput
			m.ToolCalling = known.ToolCalling
		}
	}
	if m.Source == "" {
		m.Source = c.Name
	}
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	if p := c.profile(raw); p != c {
		return p.Models(ctx, raw)
	}
	m, _, e := c.models(ctx, raw)
	return m, e
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	if p := c.profile(raw); p != c {
		return p.Verify(ctx, raw)
	}
	_, id, e := c.Normalize(raw)
	if e != nil {
		return id, e
	}
	models, static, e := c.models(ctx, raw)
	if e != nil {
		return id, e
	}
	if len(models) == 0 {
		return id, providerhttp.Invalid(c.Name, "没有返回可用模型")
	}
	if static {
		e = c.Stream(ctx, raw, provider.Request{Model: models[0].ID, Body: []byte(`{"messages":[{"role":"user","content":"Reply OK"}],"max_tokens":64}`)}, func([]byte) error { return nil })
	}
	return id, e
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	if p := c.profile(raw); p != c {
		return p.Stream(ctx, raw, r, emit)
	}
	body, e := chatwire.Encode(r, c.Name)
	if e != nil {
		return e
	}
	resp, e := c.request(ctx, raw, "POST", c.Base+"/chat/completions", body)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return c.failure(resp)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken(c.Name)
	}
	return chatwire.Decode(ctx, resp.Body, c.Name, emit)
}

func (c *Client) failure(resp *http.Response) error {
	e := providerhttp.FailureResponse(c.Name, resp).(*provider.Error)
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if cnProviderResponseIndicatesInsufficientBalance(b) {
		e.Code = "INSUFFICIENT_BALANCE"
		e.Message = c.Name + " 余额不足"
		e.AccountFault = false
	}
	return e
}
