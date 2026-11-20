package kiro

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"codearts2api/internal/provider"
)

func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	r, ok := c.Regions[a.Region]
	if !ok {
		return nil, invalid("region 无效")
	}
	catalog := r.Catalog
	if a.APIKey != "" {
		// API keys use the regional Q service, not the OAuth metadata host.
		catalog = r.Runtime
	}
	out := map[string]provider.Model{}
	next := ""
	seen := map[string]bool{}
	for page := 0; page < 20; page++ {
		body := map[string]any{"origin": "AI_EDITOR"}
		if a.ProfileARN != "" {
			body["profileArn"] = a.ProfileARN
		}
		if next != "" {
			body["nextToken"] = next
		}
		resp, e := c.request(ctx, http.MethodPost, catalog+"/", a, "AmazonCodeWhispererService.ListAvailableModels", body)
		if e != nil {
			return nil, e
		}
		o, e := readJSON(resp)
		if e != nil {
			return nil, e
		}
		var models []struct {
			ID     string `json:"modelId"`
			Name   string `json:"modelName"`
			Limits struct {
				Input  int `json:"maxInputTokens"`
				Output int `json:"maxOutputTokens"`
			} `json:"tokenLimits"`
		}
		if json.Unmarshal(o["models"], &models) != nil {
			return nil, broken()
		}
		for _, m := range models {
			if m.ID == "" || len(m.ID) > 200 || strings.ContainsAny(m.ID, "/\r\n\x00") || m.Limits.Input < 0 || m.Limits.Output < 0 {
				continue
			}
			if m.Name == "" {
				m.Name = m.ID
			}
			out[m.ID] = provider.Model{ID: m.ID, Name: m.Name, Context: m.Limits.Input, MaxOutput: m.Limits.Output, Provider: provider.Kiro, Scope: a.Region, UpstreamID: m.ID, Source: "Kiro", StreamOnly: true}
		}
		next = field(o, "nextToken")
		if next == "" {
			result := make([]provider.Model, 0, len(out))
			for _, m := range out {
				result = append(result, m)
			}
			sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
			return result, nil
		}
		if seen[next] || len(next) > 32768 {
			return nil, broken()
		}
		seen[next] = true
	}
	return nil, broken()
}
