// SPDX-License-Identifier: LGPL-3.0-only
// Adapted Sub2API protocol mappings/test cases; see UPSTREAM.md.
package cnapi

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Credit follows the pinned Sub2API balance field mappings. Unlike its Kimi
// fallback, a missing/invalid amount is never converted into a zero balance.
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if p := c.profile(raw); p != c {
		return p.Credit(ctx, raw)
	}
	if c.mode() == "coding" {
		if c.ID == "kimi" {
			return c.kimiCodingCredit(ctx, raw)
		}
		return c.codingCredit(ctx, raw)
	}
	if _, _, e := c.Normalize(raw); e != nil {
		return nil, e
	}
	out := accounts.CreditInfo{Kind: "unsupported", Reason: "官方未提供可用余额查询", UpdatedAt: time.Now().Unix()}
	if c.BalanceURL == "" {
		return json.Marshal(out)
	}
	resp, e := c.request(ctx, raw, "GET", c.BalanceURL, nil)
	if e != nil {
		return nil, e
	}
	o, e := providerhttp.JSON(resp, c.Name)
	if e != nil {
		return nil, e
	}
	out.Kind = "windows"
	out.Reason = ""
	if c.ID == "kimi" {
		if code := o["code"]; len(code) > 0 && string(code) != "0" {
			return nil, providerhttp.Broken(c.Name)
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(o["data"], &data) != nil {
			return nil, providerhttp.Broken(c.Name)
		}
		amount, e := accounts.QuotaNumber(data, "available_balance")
		if e != nil || amount == nil {
			return nil, providerhttp.Broken(c.Name)
		}
		out.Windows = []accounts.QuotaWindow{{ID: "balance-CNY", Name: "余额", Unit: "CNY", Remaining: amount}}
	} else {
		var rows []map[string]json.RawMessage
		if json.Unmarshal(o["balance_infos"], &rows) != nil || len(rows) == 0 {
			return nil, providerhttp.Broken(c.Name)
		}
		seen := map[string]bool{}
		for _, row := range rows {
			unit := strings.ToUpper(providerhttp.Field(row, "currency"))
			amount, e := accounts.QuotaNumber(row, "total_balance")
			if e != nil || amount == nil || unit == "" || seen[unit] {
				return nil, providerhttp.Broken(c.Name)
			}
			seen[unit] = true
			out.Windows = append(out.Windows, accounts.QuotaWindow{ID: "balance-" + unit, Name: "余额", Unit: unit, Remaining: amount})
		}
	}
	out.Available = true
	return json.Marshal(out)
}
