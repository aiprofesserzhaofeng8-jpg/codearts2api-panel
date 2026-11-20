// SPDX-License-Identifier: LGPL-3.0-only
// OpenCode mode/endpoint design adapted from Sub2API; see UPSTREAM.md.
package opencode

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

const origin = "https://opencode.ai/inference"

type Client struct {
	HTTP   *http.Client
	Origin string
}

func New() *Client { return &Client{HTTP: providerhttp.New(), Origin: origin} }

type credentials struct {
	Rules    []provider.ProtocolRule `json:"protocol_rules"`
	Protocol provider.Protocol       `json:"api_protocol,omitempty"`
	Key      string                  `json:"api_key"`
	Scope    string                  `json:"scope"`
	Mode     string                  `json:"account_mode"`
	Auth     string                  `json:"auth_mode"`
	Name     string                  `json:"name,omitempty"`
}

func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var a credentials
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(raw, &fields) != nil || fields == nil || a.Key == "" || strings.TrimSpace(a.Key) != a.Key || !providerhttp.Safe(a.Key, a.Name) || (a.Scope != "zen" && a.Scope != "go") || (a.Auth != "" && a.Auth != "api_key") {
		return nil, provider.Identity{}, providerhttp.Invalid("OpenCode", "需要 API Key 和明确的 scope=zen 或 go")
	}
	mode := "payg"
	if a.Scope == "go" {
		mode = "subscription"
	}
	if a.Mode != "" && a.Mode != mode {
		return nil, provider.Identity{}, providerhttp.Invalid("OpenCode", "套餐与范围不匹配")
	}
	for _, key := range []string{"access_token", "refresh_token", "base_url", "api_base_urls", "password", "cookie"} {
		if b := fields[key]; len(b) > 0 && string(b) != "null" && string(b) != `""` {
			return nil, provider.Identity{}, providerhttp.Invalid("OpenCode", "不支持混合登录材料或自定义端点")
		}
	}
	a.Mode = mode
	if a.Protocol != "" && a.Protocol != provider.AdaptiveProtocol && a.Protocol != provider.ChatProtocol && a.Protocol != provider.ResponsesProtocol && a.Protocol != provider.MessagesProtocol {
		return nil, provider.Identity{}, providerhttp.Invalid("opencode", "API 协议无效")
	}
	if err := provider.ValidateProtocolRules(a.Rules); err != nil {
		return nil, provider.Identity{}, providerhttp.Invalid("opencode", err.Error())
	}
	a.Auth = "api_key"
	sum := sha256.Sum256([]byte("opencode:" + a.Scope + ":" + a.Key))
	name := a.Name
	if name == "" {
		name = "OpenCode " + a.Scope
	}
	b, err := json.Marshal(a)
	return b, provider.Identity{ID: hex.EncodeToString(sum[:16]), Scope: a.Scope, Name: name, NonExpiring: true}, err
}
func (c *Client) Refresh(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	return b, err
}
func (c *Client) base(scope string) string {
	if scope == "go" {
		return c.Origin + "/go"
	}
	return c.Origin
}
func (c *Client) get(ctx context.Context, raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	resp, err := providerhttp.Do(ctx, c.HTTP, "OpenCode", "GET", c.base(a.Scope)+path, http.Header{"Authorization": {"Bearer " + a.Key}, "User-Agent": {"codearts2api-panel/opencode"}}, nil)
	if err != nil {
		return nil, err
	}
	return providerhttp.JSON(resp, "OpenCode")
}
