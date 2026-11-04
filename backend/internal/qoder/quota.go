package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
)

const QuotaPath = "/api/v2/quota/usage"

func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	resp, e := c.authRequest(ctx, a.Scope, QuotaPath, http.MethodGet, a.AccessToken, nil)
	if e != nil {
		return nil, e
	}
	b, e := readJSON(resp)
	if e != nil {
		return nil, e
	}
	var o map[string]json.RawMessage
	if json.Unmarshal(b, &o) != nil || o == nil {
		return nil, failure(502)
	}
	if code := o["code"]; len(code) > 0 && string(code) != "0" && string(code) != `"0"` && string(code) != "200" && string(code) != `"200"` && string(code) != "null" {
		return nil, failure(502)
	}
	if v := o["success"]; string(v) == "false" {
		return nil, failure(502)
	}
	if data := o["data"]; len(data) > 0 && string(data) != "null" {
		o = nil
		if json.Unmarshal(data, &o) != nil || o == nil {
			return nil, failure(502)
		}
	}
	return parseQuota(o, time.Now().Unix())
}
func parseQuota(o map[string]json.RawMessage, now int64) (json.RawMessage, error) {
	info := accounts.CreditInfo{Kind: "credits", Unit: "credits", UpdatedAt: now, PackageName: providerhttp.Field(o, "userType", "planName")}
	for _, def := range []struct{ ID, Name string }{{"userQuota", "套餐额度"}, {"addOnQuota", "附加额度"}, {"orgResourcePackage", "组织共享额度"}} {
		b := o[def.ID]
		if len(b) == 0 || string(b) == "null" {
			continue
		}
		var v map[string]json.RawMessage
		if json.Unmarshal(b, &v) != nil || v == nil {
			return nil, failure(502)
		}
		if a := v["available"]; len(a) > 0 && string(a) != "true" {
			continue
		}
		if unit := strings.ToLower(providerhttp.Field(v, "unit")); unit != "" && unit != "credit" && unit != "credits" {
			continue
		}
		used, e := accounts.QuotaNumber(v, "used")
		if e != nil {
			return nil, failure(502)
		}
		total, e := accounts.QuotaNumber(v, "total", "cap")
		if e != nil {
			return nil, failure(502)
		}
		remaining, e := accounts.QuotaNumber(v, "remaining")
		if e != nil {
			return nil, failure(502)
		}
		w := accounts.QuotaWindow{ID: def.ID, Name: def.Name, Unit: "credits", Used: used, Total: total, Remaining: remaining}
		if def.ID == "orgResourcePackage" && total != nil && *total == -1 {
			w.Unlimited = true
			w.Total = nil
			w.Remaining = nil
		}
		// Some quota percentages are ratios, others are percentages. Derive only
		// from explicit amounts rather than guessing the upstream percentage scale.
		for _, key := range []string{"expiresAt", "expires_at", "expireAt"} {
			if at := providerhttp.Timestamp(v[key]); at > 0 && at < 253402300800 {
				w.ExpiresAt = at
				break
			}
		}
		if w.ExpiresAt > 0 && w.ExpiresAt <= now {
			continue
		}
		if used == nil && total == nil && remaining == nil {
			continue
		}
		if !w.Complete() {
			return nil, failure(502)
		}
		info.Windows = append(info.Windows, w)
	}
	info.Available = len(info.Windows) > 0
	info.SummarizeCredits()
	if string(o["isQuotaExceeded"]) == "true" && info.Total == 0 && info.Remaining == 0 {
		finite := true
		for _, w := range info.Windows {
			if w.Unlimited || w.Total != nil && *w.Total > 0 || w.Remaining != nil && *w.Remaining > 0 || w.Used != nil && *w.Used > 0 {
				finite = false
			}
		}
		if finite {
			info.Available = false
			info.Windows = nil
			info.Reason = "上游返回全零且标记超限的空额度，不能据此判断真实余额"
		}
	}
	if !info.Available && info.Reason == "" {
		info.Reason = "上游未返回 Qoder 额度明细，无法推断剩余额度"
	}
	return json.Marshal(info)
}
