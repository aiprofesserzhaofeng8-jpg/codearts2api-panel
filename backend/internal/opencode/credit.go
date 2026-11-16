// SPDX-License-Identifier: LGPL-3.0-only
// Usage window fields adapted from Sub2API opencode_go_usage.go; see UPSTREAM.md.
package opencode

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"time"
)

func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	out := accounts.CreditInfo{Kind: "windows", UpdatedAt: time.Now().Unix(), Reason: "上游未提供可用额度"}
	if a.Scope == "zen" {
		out.Kind = "unsupported"
		out.Reason = "Zen 按量余额接口尚未接入"
		return json.Marshal(out)
	}
	obj, err := c.get(ctx, raw, "/v1/usage")
	if err != nil {
		return nil, err
	}
	if raw, ok := obj["usage"]; ok {
		if string(raw) == "null" {
			return json.Marshal(out)
		}
		var wrapped map[string]json.RawMessage
		if json.Unmarshal(raw, &wrapped) != nil || wrapped == nil {
			return nil, providerhttp.Broken("OpenCode")
		}
		obj = wrapped
	}
	for _, key := range []string{"rolling", "weekly", "monthly"} {
		raw := obj[key]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var window map[string]json.RawMessage
		if json.Unmarshal(raw, &window) != nil || window == nil {
			return nil, providerhttp.Broken("OpenCode")
		}
		percent, err := accounts.QuotaNumber(window, "percent")
		if err != nil {
			return nil, providerhttp.Broken("OpenCode")
		}
		if percent == nil {
			continue
		}
		w := accounts.QuotaWindow{ID: key, Name: "OpenCode Go " + key, Unit: "percent", Utilization: percent}
		if value := providerhttp.Field(window, "resetsAt"); value != "" {
			at, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return nil, providerhttp.Broken("OpenCode")
			}
			w.ResetsAt = at.Unix()
		}
		if !w.Complete() {
			return nil, providerhttp.Broken("OpenCode")
		}
		out.Windows = append(out.Windows, w)
	}
	out.Available = len(out.Windows) > 0
	if out.Available {
		out.Reason = ""
	}
	return json.Marshal(out)
}
