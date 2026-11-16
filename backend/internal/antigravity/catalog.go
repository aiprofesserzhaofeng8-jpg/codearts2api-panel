package antigravity

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

func (c *Client) catalog(ctx context.Context, raw json.RawMessage) (map[string]json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	obj, err := c.cliCall(ctx, a, "POST", "/v1internal:fetchAvailableModels", FetchAvailableModelsRequest{Project: a.Project})
	if err != nil {
		return nil, err
	}
	var models map[string]json.RawMessage
	if json.Unmarshal(obj["models"], &models) != nil || models == nil || len(models) > 2048 {
		return nil, providerhttp.Broken("Antigravity")
	}
	return models, nil
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	rows, err := c.catalog(ctx, raw)
	if err != nil {
		return nil, err
	}
	out := []provider.Model{}
	for id, b := range rows {
		var m ModelInfo
		if !providerhttp.ModelID(id) || json.Unmarshal(b, &m) != nil || string(b) == "null" {
			return nil, providerhttp.Broken("Antigravity")
		}
		if m.MaxTokens != nil && *m.MaxTokens < 0 || m.MaxOutputTokens != nil && *m.MaxOutputTokens < 0 {
			return nil, providerhttp.Broken("Antigravity")
		}
		// Media generation has no gateway representation in this integration.
		if strings.Contains(id, "image") || strings.Contains(id, "tts") || strings.Contains(id, "audio") {
			continue
		}
		item := provider.Model{ID: id, UpstreamID: id, Name: m.DisplayName, Provider: "antigravity", Scope: "global", StreamOnly: true, Source: "Antigravity fetchAvailableModels"}
		if item.Name == "" {
			item.Name = id
		}
		if m.MaxTokens != nil {
			item.Context = *m.MaxTokens
		}
		if m.MaxOutputTokens != nil {
			item.MaxOutput = *m.MaxOutputTokens
		}
		if m.SupportsImages != nil {
			item.ImageInput = "unsupported"
			if *m.SupportsImages {
				item.ImageInput = "supported"
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	rows, err := c.catalog(ctx, raw)
	if err != nil {
		return nil, err
	}
	out := accounts.CreditInfo{Kind: "windows", UpdatedAt: time.Now().Unix(), Reason: "上游未返回可用配额"}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, id := range keys {
		var m map[string]json.RawMessage
		if !providerhttp.ModelID(id) || json.Unmarshal(rows[id], &m) != nil || m == nil {
			return nil, providerhttp.Broken("Antigravity")
		}
		q := m["quotaInfo"]
		if len(q) == 0 || string(q) == "null" {
			continue
		}
		var quota map[string]json.RawMessage
		if json.Unmarshal(q, &quota) != nil || quota == nil {
			return nil, providerhttp.Broken("Antigravity")
		}
		fraction, err := accounts.QuotaNumber(quota, "remainingFraction")
		if err != nil || fraction != nil && (*fraction < 0 || *fraction > 1) {
			return nil, providerhttp.Broken("Antigravity")
		}
		if fraction == nil {
			continue
		}
		used := (1 - *fraction) * 100
		w := accounts.QuotaWindow{ID: id, Name: id, Unit: "percent", Utilization: &used}
		if reset := providerhttp.Field(quota, "resetTime"); reset != "" {
			at, err := time.Parse(time.RFC3339, reset)
			if err != nil {
				return nil, providerhttp.Broken("Antigravity")
			}
			w.ResetsAt = at.Unix()
		}
		out.Windows = append(out.Windows, w)
	}
	out.Available = len(out.Windows) > 0
	if out.Available {
		out.Reason = ""
	}
	return json.Marshal(out)
}
