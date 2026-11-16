// SPDX-License-Identifier: LGPL-3.0-only
// Billing endpoint/units follow Sub2API pkg/xai/billing.go; see UPSTREAM.md.
package grok

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"strings"
	"time"
)

func billingNumber(obj map[string]json.RawMessage, key string) (*float64, error) {
	raw := obj[key]
	if len(raw) > 0 && strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		var value map[string]json.RawMessage
		if json.Unmarshal(raw, &value) != nil {
			return nil, providerhttp.Broken("Grok")
		}
		if len(value["val"]) == 0 {
			return nil, providerhttp.Broken("Grok")
		}
		return accounts.QuotaNumber(value, "val")
	}
	return accounts.QuotaNumber(obj, key)
}
func billingWindows(obj map[string]json.RawMessage, weekly bool) ([]accounts.QuotaWindow, error) {
	var cfg map[string]json.RawMessage
	if raw := obj["config"]; len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	} else if json.Unmarshal(raw, &cfg) != nil || cfg == nil {
		return nil, providerhttp.Broken("Grok")
	}
	out := []accounts.QuotaWindow{}
	appendWindow := func(w accounts.QuotaWindow, reset string) error {
		if reset != "" {
			at, err := time.Parse(time.RFC3339, reset)
			if err != nil {
				return providerhttp.Broken("Grok")
			}
			w.ResetsAt = at.Unix()
		}
		if !w.Complete() {
			return providerhttp.Broken("Grok")
		}
		out = append(out, w)
		return nil
	}
	if weekly {
		var period map[string]json.RawMessage
		if b := cfg["currentPeriod"]; len(b) > 0 && string(b) != "null" {
			if json.Unmarshal(b, &period) != nil {
				return nil, providerhttp.Broken("Grok")
			}
		}
		used, err := accounts.QuotaNumber(cfg, "creditUsagePercent")
		if err != nil {
			return nil, providerhttp.Broken("Grok")
		}
		if used != nil {
			if err = appendWindow(accounts.QuotaWindow{ID: "weekly", Name: "Grok 订阅周期", Unit: "percent", Utilization: used}, providerhttp.Field(period, "end")); err != nil {
				return nil, err
			}
		}
	} else {
		limit, e1 := billingNumber(cfg, "monthlyLimit")
		used, e2 := billingNumber(cfg, "used")
		if e1 != nil || e2 != nil {
			return nil, providerhttp.Broken("Grok")
		}
		// Only these two fields are cents. Do not infer a plan or add dollar-valued
		// prepaid/on-demand amounts into the monthly subscription window.
		if limit != nil {
			v := *limit / 100
			limit = &v
		}
		if used != nil {
			v := *used / 100
			used = &v
		}
		if limit != nil || used != nil {
			if err := appendWindow(accounts.QuotaWindow{ID: "monthly", Name: "Grok 月度额度", Unit: "USD", Total: limit, Used: used}, providerhttp.Field(cfg, "billingPeriodEnd")); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	out := accounts.CreditInfo{Kind: "windows", UpdatedAt: time.Now().Unix(), Reason: "上游未提供订阅窗口"}
	for _, weekly := range []bool{true, false} {
		endpoint := c.BaseURL + "/billing"
		if weekly {
			endpoint += "?format=credits"
		}
		resp, err := providerhttp.Do(ctx, c.HTTP, "Grok", "GET", endpoint, cliHeaders(a.Access), nil)
		if err != nil {
			return nil, err
		}
		obj, err := providerhttp.JSON(resp, "Grok")
		if err != nil {
			return nil, err
		}
		windows, err := billingWindows(obj, weekly)
		if err != nil {
			return nil, err
		}
		out.Windows = append(out.Windows, windows...)
	}
	out.Available = len(out.Windows) > 0
	if out.Available {
		out.Reason = ""
	}
	return json.Marshal(out)
}
