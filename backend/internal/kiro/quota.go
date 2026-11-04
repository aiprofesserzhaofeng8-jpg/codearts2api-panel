package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
)

func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	r, ok := c.Regions[a.Region]
	if !ok {
		return nil, invalid("region 无效")
	}
	q := url.Values{"origin": {"AI_EDITOR"}, "resourceType": {"AGENTIC_REQUEST"}}
	if a.ProfileARN != "" {
		q.Set("profileArn", a.ProfileARN)
	}
	resp, e := c.request(ctx, http.MethodGet, r.Runtime+"/getUsageLimits?"+q.Encode(), a, "", nil)
	if e != nil {
		return nil, e
	}
	o, e := readJSON(resp)
	if e != nil {
		return nil, e
	}
	return parseQuota(o, time.Now().Unix())
}
func parseQuota(o map[string]json.RawMessage, now int64) (json.RawMessage, error) {
	info := accounts.CreditInfo{Kind: "credits", Unit: "credits", UpdatedAt: now}
	var subscription map[string]json.RawMessage
	_ = json.Unmarshal(o["subscriptionInfo"], &subscription)
	info.PackageName = field(subscription, "subscriptionTitle", "subscriptionType")
	var rows []map[string]json.RawMessage
	if b := o["usageBreakdownList"]; len(b) > 0 && string(b) != "null" {
		if json.Unmarshal(b, &rows) != nil || len(rows) > 32 {
			return nil, broken()
		}
	}
	if len(rows) == 0 {
		info.Reason = "上游未返回 Kiro Credits 用量明细，无法推断剩余额度"
		return json.Marshal(info)
	}
	// The AGENTIC_REQUEST contract returns the credit resource first. Prefer an
	// explicitly identified credit resource when several resources are returned.
	row := rows[0]
	for _, v := range rows {
		if strings.EqualFold(field(v, "unit"), "CREDITS") || field(v, "resourceType") == "AGENTIC_REQUEST" {
			row = v
			break
		}
	}
	if unit := strings.ToLower(field(row, "unit")); unit != "" && unit != "credit" && unit != "credits" {
		info.Reason = "上游未返回 Kiro Credits 数量，无法推断剩余额度"
		return json.Marshal(info)
	}
	reset := quotaTime(o["nextDateReset"])
	if reset == 0 {
		reset = quotaTime(row["nextDateReset"])
	}
	appendBucket := func(v map[string]json.RawMessage, id, name string, expiry int64) error {
		used, e := accounts.QuotaNumber(v, "currentUsageWithPrecision", "currentUsage")
		if e != nil {
			return broken()
		}
		total, e := accounts.QuotaNumber(v, "usageLimitWithPrecision", "usageLimit")
		if e != nil {
			return broken()
		}
		if used == nil && total == nil {
			return nil
		}
		w := accounts.QuotaWindow{ID: id, Name: name, Unit: "credits", Used: used, Total: total, ExpiresAt: expiry}
		if id == "plan" {
			w.ResetsAt = reset
		}
		if !w.Complete() {
			return broken()
		}
		info.Windows = append(info.Windows, w)
		return nil
	}
	if e := appendBucket(row, "plan", "套餐额度", 0); e != nil {
		return nil, e
	}
	if b := row["freeTrialInfo"]; len(b) > 0 && string(b) != "null" {
		var trial map[string]json.RawMessage
		if json.Unmarshal(b, &trial) != nil {
			return nil, broken()
		}
		expiry := quotaTime(trial["freeTrialExpiry"])
		if strings.EqualFold(field(trial, "freeTrialStatus"), "ACTIVE") && (expiry == 0 || expiry > now) {
			if e := appendBucket(trial, "trial", "试用额度", expiry); e != nil {
				return nil, e
			}
		}
	}
	if b := row["bonuses"]; len(b) > 0 && string(b) != "null" {
		var bonuses []map[string]json.RawMessage
		if json.Unmarshal(b, &bonuses) != nil || len(bonuses) > 64 {
			return nil, broken()
		}
		for i, bonus := range bonuses {
			expiry := quotaTime(bonus["expiresAt"])
			if !strings.EqualFold(field(bonus, "status"), "ACTIVE") || expiry > 0 && expiry <= now {
				continue
			}
			if e := appendBucket(bonus, fmt.Sprintf("bonus_%d", i), "赠送额度", expiry); e != nil {
				return nil, e
			}
		}
	}
	info.Available = len(info.Windows) > 0
	info.SummarizeCredits()
	if !info.Available {
		info.Reason = "上游未返回 Kiro Credits 数量，无法推断剩余额度"
	}
	return json.Marshal(info)
}
func quotaTime(b json.RawMessage) int64 {
	if n := providerhttp.Timestamp(b); n > 0 {
		return n
	}
	if v, e := accounts.QuotaNumber(map[string]json.RawMessage{"at": b}, "at"); e == nil && v != nil && *v > 0 && *v < 253402300800 {
		return int64(*v)
	}
	return 0
}
