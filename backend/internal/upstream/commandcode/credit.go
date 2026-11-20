// SPDX-License-Identifier: LGPL-3.0-only
// Alpha account and quota fields adapted from Sub2API command_code_usage.go.
package commandcode

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	identity, err := c.get(ctx, raw, "/alpha/whoami")
	if err != nil {
		return nil, err
	}
	// An unknown organization must not silently fall back to personal billing.
	orgRaw, ok := identity["org"]
	if !ok {
		return nil, providerhttp.Broken("Command Code")
	}
	query := ""
	if string(orgRaw) != "null" {
		var org map[string]json.RawMessage
		if json.Unmarshal(orgRaw, &org) != nil || org == nil {
			return nil, providerhttp.Broken("Command Code")
		}
		id := providerhttp.Field(org, "id")
		if id == "" || !providerhttp.Safe(id) {
			return nil, providerhttp.Broken("Command Code")
		}
		query = "?" + url.Values{"orgId": {id}}.Encode()
	}
	obj, err := c.get(ctx, raw, "/alpha/billing/credits"+query)
	if err != nil {
		return nil, err
	}
	var credits map[string]json.RawMessage
	if json.Unmarshal(obj["credits"], &credits) != nil || credits == nil {
		return nil, providerhttp.Broken("Command Code")
	}
	out := accounts.CreditInfo{Kind: "windows", UpdatedAt: time.Now().Unix(), Reason: "上游未返回可用额度"}
	for _, key := range []string{"monthlyCredits", "purchasedCredits", "freeCredits"} {
		n, err := accounts.QuotaNumber(credits, key)
		if err != nil {
			return nil, providerhttp.Broken("Command Code")
		}
		if n == nil {
			continue
		}
		w := accounts.QuotaWindow{ID: key, Name: "Command Code " + key, Remaining: n, Unit: "USD"}
		if !w.Complete() {
			return nil, providerhttp.Broken("Command Code")
		}
		out.Windows = append(out.Windows, w)
	}
	if b := obj["windowLimits"]; len(b) > 0 && string(b) != "null" {
		var windows map[string]json.RawMessage
		if json.Unmarshal(b, &windows) != nil || windows == nil {
			return nil, providerhttp.Broken("Command Code")
		}
		var limited *bool
		if json.Unmarshal(windows["limited"], &limited) != nil || limited == nil {
			return nil, providerhttp.Broken("Command Code")
		}
		if *limited {
			for _, key := range []string{"fiveHour", "weekly"} {
				b := windows[key]
				if len(b) == 0 || string(b) == "null" {
					continue
				}
				var value map[string]json.RawMessage
				if json.Unmarshal(b, &value) != nil || value == nil {
					return nil, providerhttp.Broken("Command Code")
				}
				used, e1 := accounts.QuotaNumber(value, "used")
				total, e2 := accounts.QuotaNumber(value, "cap")
				reset, e3 := accounts.QuotaNumber(value, "resetAt")
				if e1 != nil || e2 != nil || e3 != nil {
					return nil, providerhttp.Broken("Command Code")
				}
				w := accounts.QuotaWindow{ID: key, Name: "Command Code " + key, Unit: "USD", Used: used, Total: total}
				if reset != nil {
					if *reset < 0 || *reset > 1e15 {
						return nil, providerhttp.Broken("Command Code")
					}
					w.ResetsAt = int64(*reset / 1000)
				}
				if used == nil && total == nil {
					continue
				}
				if !w.Complete() {
					return nil, providerhttp.Broken("Command Code")
				}
				out.Windows = append(out.Windows, w)
			}
		}
	}
	// Fetch period details only when a monthly pool exists. Do not guess total
	// spend from unrelated purchased/free credits or invent a monthly reset.
	partial := false
	for i := range out.Windows {
		if out.Windows[i].ID == "monthlyCredits" {
			if err := c.monthly(ctx, raw, query, &out.Windows[i]); err != nil {
				var upstream *provider.Error
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &upstream) && (upstream.Status == 401 || upstream.Status == 403) {
					return nil, err
				}
				partial = true
			}
		}
	}
	// Display only: prepaid credits may bypass subscription windows upstream.
	// Never use this snapshot for gateway admission, cooldown or billing.
	out.Available = len(out.Windows) > 0
	if out.Available {
		out.Reason = ""
	}
	if partial {
		out.Reason = "月度周期详情暂不可用；已返回的余额和滚动窗口保留"
	}
	return json.Marshal(out)
}

func (c *Client) monthly(ctx context.Context, raw json.RawMessage, query string, w *accounts.QuotaWindow) error {
	obj, err := c.get(ctx, raw, "/alpha/billing/subscriptions"+query)
	if err != nil {
		return err
	}
	if b, ok := obj["data"]; ok && string(b) == "null" {
		return nil
	}
	var period map[string]json.RawMessage
	if json.Unmarshal(obj["data"], &period) != nil || period == nil {
		return providerhttp.Broken("Command Code")
	}
	start, end := providerhttp.Field(period, "currentPeriodStart"), providerhttp.Field(period, "currentPeriodEnd")
	if start == "" && end == "" {
		return nil
	}
	from, e1 := time.Parse(time.RFC3339, start)
	until, e2 := time.Parse(time.RFC3339, end)
	if e1 != nil || e2 != nil || !until.After(from) {
		return providerhttp.Broken("Command Code")
	}
	q, _ := url.ParseQuery(strings.TrimPrefix(query, "?"))
	q.Set("since", start)
	summary, err := c.get(ctx, raw, "/alpha/usage/summary?"+q.Encode())
	if err != nil {
		return err
	}
	used, err := accounts.QuotaNumber(summary, "totalMonthlyCredits")
	if err != nil {
		return providerhttp.Broken("Command Code")
	}
	// totalCost can include purchased credits; it is not proof of monthly usage.
	if used == nil {
		return providerhttp.Broken("Command Code")
	}
	total := *used + *w.Remaining
	candidate := *w
	candidate.Used = used
	candidate.Total = &total
	candidate.ResetsAt = until.Unix()
	if !candidate.Complete() {
		return providerhttp.Broken("Command Code")
	}
	*w = candidate
	return nil
}
