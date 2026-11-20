package cline

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

func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	obj, err := c.get(ctx, raw, "/api/v1/models")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Context int    `json:"context_length"`
		Output  int    `json:"max_output_tokens"`
		Images  *bool  `json:"supportsImages"`
		Tools   *bool  `json:"supportsTools"`
	}
	if json.Unmarshal(obj["data"], &rows) != nil || rows == nil || len(rows) > 4096 {
		return nil, providerhttp.Broken("Cline")
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	for _, r := range rows {
		if !validModel(r.ID) || r.Context < 0 || r.Output < 0 {
			return nil, providerhttp.Broken("Cline")
		}
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		name := r.Name
		if name == "" {
			name = r.ID
		}
		m := provider.Model{ID: r.ID, UpstreamID: r.ID, Name: name, Provider: "cline", Scope: "global", Context: r.Context, MaxOutput: r.Output, StreamOnly: true, Source: c.Origin + "/api/v1/models"}
		if r.Images != nil {
			m.ImageInput = "unsupported"
			if *r.Images {
				m.ImageInput = "supported"
			}
		}
		if r.Tools != nil {
			m.ToolCalling = "unsupported"
			if *r.Tools {
				m.ToolCalling = "supported"
			}
		}
		out = append(out, m)
	}
	return out, nil
}
