// SPDX-License-Identifier: LGPL-3.0-only
// Protocol choices adapted from Sub2API command_code.go; see UPSTREAM.md.
package commandcode

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"strings"
	"unicode"
)

func validModel(id string) bool {
	if id == "" || len(id) > 200 || strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	for _, part := range strings.Split(id, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func nativePath(id string) string {
	base := strings.ToLower(id)
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if strings.HasPrefix(base, "jev") {
		return ""
	}
	if strings.HasPrefix(base, "claude-") {
		return "/messages"
	}
	if strings.HasPrefix(base, "gpt-") {
		return "/responses"
	}
	return "/chat/completions"
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	obj, err := c.get(ctx, raw, "/provider/v1/models")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		Endpoints  []string `json:"supported_endpoints"`
		Context    int      `json:"context_length"`
		Output     int      `json:"max_output_tokens"`
		Modalities struct {
			Input []string `json:"input"`
		} `json:"modalities"`
	}
	if json.Unmarshal(obj["data"], &rows) != nil || rows == nil || len(rows) > 4096 {
		return nil, providerhttp.Broken("Command Code")
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	for _, r := range rows {
		if !validModel(r.ID) || r.Context < 0 || r.Output < 0 {
			return nil, providerhttp.Broken("Command Code")
		}
		path := nativePath(r.ID)
		if path == "" || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		match := false
		for _, e := range r.Endpoints {
			if e == path || e == "/v1"+path || e == "/provider/v1"+path {
				match = true
			}
		}
		if !match {
			continue
		}
		name := r.Name
		if name == "" {
			name = r.ID
		}
		m := provider.Model{ID: r.ID, UpstreamID: r.ID, Name: name, Provider: "commandcode", Scope: "global", Context: r.Context, MaxOutput: r.Output, StreamOnly: true, Source: c.Origin + "/provider/v1/models"}
		if r.Modalities.Input != nil {
			m.ImageInput = "unsupported"
			for _, v := range r.Modalities.Input {
				if v == "image" || strings.HasPrefix(v, "image/") {
					m.ImageInput = "supported"
				}
			}
		}
		out = append(out, m)
	}
	return out, nil
}
