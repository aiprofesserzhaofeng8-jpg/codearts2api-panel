package opencode

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"strings"
)

// Models is public metadata discovery, not API-key or subscription verification.
// Verify must use an authenticated operation before account import is enabled.
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	obj, err := c.get(ctx, raw, "/v1/models")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Type  string `json:"type"`
		Limit struct {
			Context int `json:"context"`
			Output  int `json:"output"`
		} `json:"limit"`
		Modalities struct {
			Input  []string `json:"input"`
			Output []string `json:"output"`
		} `json:"modalities"`
		Capabilities struct {
			Tools struct {
				Supported *bool `json:"supported"`
			} `json:"tools"`
		} `json:"capabilities"`
		Cost struct {
			Text struct {
				Input struct {
					Price *float64 `json:"price"`
				} `json:"input"`
				Output struct {
					Price *float64 `json:"price"`
				} `json:"output"`
			} `json:"text"`
		} `json:"cost"`
		API map[string]json.RawMessage `json:"api"`
	}
	if json.Unmarshal(obj["data"], &rows) != nil || rows == nil || len(rows) > 4096 {
		return nil, providerhttp.Broken("OpenCode")
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	for _, r := range rows {
		if !providerhttp.ModelID(r.ID) || r.Limit.Context < 0 || r.Limit.Output < 0 {
			return nil, providerhttp.Broken("OpenCode")
		}
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		p, ok := modelProtocol(a.Scope, r.ID)
		if !ok || r.Type != "chat" {
			continue
		}
		// Do not trust an upstream URL or advertise a route contradicted by its
		// live metadata. Protocol changes need a reviewed table update, not a retry.
		if _, ok = r.API[string(p)]; !ok {
			continue
		}
		if a.Scope == "zen" && (strings.HasSuffix(r.ID, "-free") || r.Cost.Text.Input.Price != nil && r.Cost.Text.Output.Price != nil && *r.Cost.Text.Input.Price == 0 && *r.Cost.Text.Output.Price == 0) {
			continue
		}
		name := r.Name
		if name == "" {
			name = r.ID
		}
		m := provider.Model{ID: r.ID, UpstreamID: r.ID, Name: name, Provider: "opencode", Scope: a.Scope, Context: r.Limit.Context, MaxOutput: r.Limit.Output, StreamOnly: true, Source: c.base(a.Scope) + "/v1/models"}
		if r.Modalities.Input != nil {
			m.ImageInput = "unsupported"
			for _, typ := range r.Modalities.Input {
				if typ == "image" || strings.HasPrefix(typ, "image/") {
					m.ImageInput = "supported"
				}
			}
		}
		if r.Capabilities.Tools.Supported != nil {
			m.ToolCalling = "unsupported"
			if *r.Capabilities.Tools.Supported {
				m.ToolCalling = "supported"
			}
		}
		out = append(out, m)
	}
	return out, nil
}
