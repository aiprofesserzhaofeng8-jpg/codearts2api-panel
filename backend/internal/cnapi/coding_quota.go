// SPDX-License-Identifier: LGPL-3.0-only
// Field mapping adapted from Sub2API parseKimiUsageTiers; see UPSTREAM.md.
package cnapi

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"time"
)

func (c *Client) kimiCodingCredit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	resp, err := c.request(ctx, raw, "GET", c.Base+"/usages", nil)
	if err != nil {
		return nil, err
	}
	obj, err := providerhttp.JSON(resp, c.Name)
	if err != nil {
		return nil, err
	}
	out := accounts.CreditInfo{Kind: "windows", UpdatedAt: time.Now().Unix(), Reason: "上游未返回套餐用量"}
	add := func(id, name string, seconds int64, b json.RawMessage) error {
		if len(b) == 0 || string(b) == "null" {
			return nil
		}
		var row map[string]json.RawMessage
		if json.Unmarshal(b, &row) != nil || row == nil {
			return providerhttp.Broken(c.Name)
		}
		total, e := accounts.QuotaNumber(row, "limit")
		if e != nil {
			return providerhttp.Broken(c.Name)
		}
		remaining, e := accounts.QuotaNumber(row, "remaining")
		if e != nil {
			return providerhttp.Broken(c.Name)
		}
		w := accounts.QuotaWindow{ID: id, Name: name, WindowSeconds: seconds, Total: total, Remaining: remaining}
		var reset string
		if json.Unmarshal(row["resetTime"], &reset) == nil {
			if at, e := time.Parse(time.RFC3339, reset); e == nil {
				w.ResetsAt = at.Unix()
			}
		}
		if total != nil && remaining != nil && *remaining > *total {
			return providerhttp.Broken(c.Name)
		}
		if w.Complete() {
			out.Windows = append(out.Windows, w)
		} else if total != nil || remaining != nil {
			return providerhttp.Broken(c.Name)
		}
		return nil
	}
	var limits []struct {
		Detail json.RawMessage `json:"detail"`
	}
	if b := obj["limits"]; len(b) > 0 && string(b) != "null" {
		if json.Unmarshal(b, &limits) != nil {
			return nil, providerhttp.Broken(c.Name)
		}
		for _, l := range limits {
			if len(l.Detail) > 0 && string(l.Detail) != "null" {
				if err = add("5h", "5小时窗口", 18000, l.Detail); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	if err = add("weekly", "7天窗口", 604800, obj["usage"]); err != nil {
		return nil, err
	}
	out.Available = len(out.Windows) > 0
	if out.Available {
		out.Reason = ""
	}
	return json.Marshal(out)
}
