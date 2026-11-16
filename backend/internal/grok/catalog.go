// SPDX-License-Identifier: LGPL-3.0-only
// CLI identity follows Sub2API pkg/xai/cli_identity.go and billing.go.
package grok

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
)

const cliVersion = "1.0.46"

func cliHeaders(access string) http.Header {
	platform, arch := runtime.GOOS, runtime.GOARCH
	if platform == "darwin" {
		platform = "macos"
	}
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	case "386":
		arch = "x86"
	}
	return http.Header{"Authorization": {"Bearer " + access}, "X-XAI-Token-Auth": {"xai-grok-cli"}, "x-grok-client-version": {cliVersion}, "x-grok-client-identifier": {"grok-pager"}, "x-grok-client-mode": {"interactive"}, "x-authenticateresponse": {"authenticate-response"}, "User-Agent": {"grok-pager/" + cliVersion + " grok-shell/" + cliVersion + " (" + platform + "; " + arch + ")"}}
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	resp, err := providerhttp.Do(ctx, c.HTTP, "Grok", "GET", c.BaseURL+"/models", cliHeaders(a.Access), nil)
	if err != nil {
		return nil, err
	}
	obj, err := providerhttp.JSON(resp, "Grok")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID      string `json:"id"`
		Name    string `json:"display_name"`
		Context int    `json:"context_window"`
		Output  int    `json:"max_output_tokens"`
	}
	if json.Unmarshal(obj["data"], &rows) != nil || rows == nil || len(rows) > 2048 {
		return nil, providerhttp.Broken("Grok")
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	for _, row := range rows {
		if !providerhttp.ModelID(row.ID) || row.Context < 0 || row.Output < 0 {
			return nil, providerhttp.Broken("Grok")
		}
		if seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		if strings.Contains(row.ID, "imagine") || strings.Contains(row.ID, "image") || strings.Contains(row.ID, "video") || strings.Contains(row.ID, "audio") {
			continue
		}
		if row.Name == "" {
			row.Name = row.ID
		}
		out = append(out, provider.Model{ID: row.ID, UpstreamID: row.ID, Name: row.Name, Provider: "grok", Scope: "build", Context: row.Context, MaxOutput: row.Output, Source: "Grok Build /models", StreamOnly: true})
	}
	return out, nil
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	_, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	models, err := c.Models(ctx, raw)
	if err != nil {
		return id, err
	}
	if len(models) == 0 {
		return id, providerhttp.Invalid("Grok", "未返回可用对话模型")
	}
	return id, nil
}
