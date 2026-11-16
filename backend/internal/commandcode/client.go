// SPDX-License-Identifier: LGPL-3.0-only
// Command Code mode/endpoint design adapted from Sub2API; see UPSTREAM.md.
package commandcode

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

const origin = "https://api.commandcode.ai"

type Client struct {
	HTTP   *http.Client
	Origin string
}

func New() *Client { return &Client{HTTP: providerhttp.New(), Origin: origin} }

type credentials struct {
	Key   string `json:"api_key"`
	Scope string `json:"scope"`
	Mode  string `json:"account_mode"`
	Auth  string `json:"auth_mode"`
	Name  string `json:"name,omitempty"`
}

func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var a credentials
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(raw, &fields) != nil || fields == nil || a.Key == "" || strings.TrimSpace(a.Key) != a.Key || !providerhttp.Safe(a.Key, a.Name) || (a.Scope != "" && a.Scope != "global") || (a.Auth != "" && a.Auth != "api_key") {
		return nil, provider.Identity{}, providerhttp.Invalid("Command Code", "需要 Command Code API Key，范围须为 global")
	}
	if a.Mode != "" && a.Mode != "subscription" && a.Mode != "payg" {
		return nil, provider.Identity{}, providerhttp.Invalid("Command Code", "需要明确的 subscription 或 payg 模式")
	}
	if a.Mode == "" {
		a.Mode = "subscription"
	}
	a.Scope = "global"
	for _, key := range []string{"access_token", "refresh_token", "base_url", "api_base_urls", "password", "cookie"} {
		if b := fields[key]; len(b) > 0 && string(b) != "null" && string(b) != `""` {
			return nil, provider.Identity{}, providerhttp.Invalid("Command Code", "不支持混合登录材料或自定义端点")
		}
	}
	a.Auth = "api_key"
	sum := sha256.Sum256([]byte("commandcode:" + a.Key))
	name := a.Name
	if name == "" {
		name = "Command Code"
	}
	b, err := json.Marshal(a)
	return b, provider.Identity{ID: hex.EncodeToString(sum[:16]), Scope: a.Scope, Name: name, NonExpiring: true}, err
}
func (c *Client) Refresh(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	return b, err
}
func (c *Client) get(ctx context.Context, raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	resp, err := providerhttp.Do(ctx, c.HTTP, "Command Code", "GET", c.Origin+path, http.Header{"Authorization": {"Bearer " + a.Key}, "User-Agent": {"codearts2api-panel/commandcode"}}, nil)
	if err != nil {
		return nil, err
	}
	return providerhttp.JSON(resp, "Command Code")
}
