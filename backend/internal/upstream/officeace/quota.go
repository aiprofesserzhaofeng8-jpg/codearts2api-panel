package officeace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type subscriptionResponse struct {
	SKUs []struct {
		SKUCode string `json:"sku_code"`
		Quotas  []struct {
			SKUAttrCode string `json:"sku_attr_code"`
			QuotaValue  string `json:"quota_value"`
			UsageValue  string `json:"usage_value"`
			Remaining   string `json:"remaining_value"`
			ExpiredTime string `json:"expired_time"`
		} `json:"quotas"`
	} `json:"skus"`
	BonusSKUs []struct {
		ActivityName string  `json:"activity_name"`
		Points       float64 `json:"points"`
		CurrentValue float64 `json:"current_value"`
		ExpiredTime  string  `json:"expired_time"`
	} `json:"bonus_skus"`
}

// officeAceDayZone is the campaign day boundary. OfficeAce stamps timestamps in
// UTC while a check-in bucket turns over at 00:00 UTC+8, so every date compare
// goes through this zone.
var officeAceDayZone = time.FixedZone("UTC+8", 8*3600)

// parseOfficeAceTime reads the ISO-8601 stamps OfficeAce returns. Fractional
// seconds are optional and the value is always UTC or offset-tagged.
func parseOfficeAceTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// isDailyBonus separates the per-day check-in bucket from the one-off grants.
// The campaign id carries the cadence (`…:daily` / `…:once`); the name is only a
// fallback because it is operator-editable text.
func isDailyBonus(resourceID, name string) bool {
	r := strings.ToLower(resourceID)
	if strings.Contains(r, "daily") || strings.Contains(r, "checkin") || strings.Contains(r, "check-in") {
		return true
	}
	return strings.Contains(name, "签到") || strings.Contains(name, "每日")
}

// dailyBonus returns today's check-in bucket, if the cloud already granted one.
// A bucket exists only after a claim, which is what makes it the claim state:
// the client and this gateway redeem the same campaign, so a bucket stamped
// today means "already claimed" whatever route was used.
func dailyBonus(sub subcriptionDoc, now time.Time) (points float64, activity string, found bool) {
	today := now.In(officeAceDayZone).Format("2006-01-02")
	for _, b := range sub.BonusSKUs {
		if !isDailyBonus(b.CBCResourceID, b.ActivityName) {
			continue
		}
		at, ok := parseOfficeAceTime(b.EffectiveTime)
		if !ok || at.In(officeAceDayZone).Format("2006-01-02") != today {
			continue
		}
		name := strings.TrimSpace(b.ActivityName)
		if name == "" {
			name = "每日签到"
		}
		return parseNumber(b.Points), name, true
	}
	return 0, "", false
}

// fetchSubscription owns the signed GET and the decode. Callers decide how an
// empty document is presented; the request itself is never duplicated.
func (c *Client) fetchSubscription(ctx context.Context, cred Credentials) (subcriptionDoc, error) {
	subURL := c.SubBase + SubscriptionPath
	headers, err := signV11(cred.AccessKeyID, cred.SecretAccessKey, cred.SecurityToken, cred.ProjectID, http.MethodGet, subURL, nil, nil, "")
	if err != nil {
		return subcriptionDoc{}, fmt.Errorf("生成订阅签名失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, nil)
	if err != nil {
		return subcriptionDoc{}, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return subcriptionDoc{}, ctx.Err()
		}
		return subcriptionDoc{}, provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return subcriptionDoc{}, providerhttp.Failure("OfficeAce", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return subcriptionDoc{}, err
	}
	var sub subcriptionDoc
	if err := json.Unmarshal(bodyBytes, &sub); err != nil {
		return subcriptionDoc{}, providerhttp.Broken("OfficeAce")
	}
	return sub, nil
}

// errorDetail keeps an upstream rejection readable: the business code and the
// message the gateway sent, never the whole body.
func errorDetail(body []byte) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return ""
	}
	code := firstString(obj, "error_code", "errorCode", "code")
	msg := firstString(obj, "error_msg", "errorMsg", "message")
	if sub, ok := obj["error"]; ok && len(sub) > 0 {
		var inner map[string]json.RawMessage
		if json.Unmarshal(sub, &inner) == nil {
			if c := firstString(inner, "code", "error_code"); c != "" {
				code = c
			}
			if m := firstString(inner, "message", "error_msg"); m != "" {
				msg = m
			}
		}
	}
	switch {
	case code != "" && msg != "":
		return code + "：" + msg
	case code != "":
		return code
	default:
		return msg
	}
}

// Credit queries subscription points and bonus pools using V11-HMAC-SHA256 signing.
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	norm, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var cred Credentials
	if err := json.Unmarshal(norm, &cred); err != nil {
		return nil, err
	}

	sub, err := c.fetchSubscription(ctx, cred)
	if err != nil {
		return nil, err
	}

	// A signed-in account that never redeemed an invitation answers with the
	// envelope only: `subscribe_status` UNSUBSCRIBED and no SKUs at all. That is
	// an account state, not a query failure, so it must not be reported as an
	// empty balance the operator would read as "0 credits left".
	if isUnsubscribed(sub) {
		return json.Marshal(accounts.CreditInfo{
			Kind:        "credits",
			Unit:        "credits",
			Available:   false,
			Status:      "unsubscribed",
			Reason:      UnsubscribedReason,
			PackageName: "云果办个人套餐",
			UpdatedAt:   time.Now().Unix(),
		})
	}

	nowMs := time.Now().UnixMilli()
	creditInfo := summarizeSubscriptionDoc(sub, nowMs)

	return json.Marshal(creditInfo)
}

// UnsubscribedReason is one operator-facing sentence: what is missing and what
// to do next. It is also reused by the catalog and inference paths.
const UnsubscribedReason = "该账号已授权但尚未开通云果办服务（UNSUBSCRIBED）：请先在云果办客户端用邀请码兑换开通，开通后重新授权或刷新额度"

// isUnsubscribed reports the signed-in-but-not-provisioned state. The envelope
// carries no SKUs in that case, so the status alone decides; an account with any
// quota keeps the normal summary even if the field is absent.
func isUnsubscribed(sub subcriptionDoc) bool {
	if len(sub.SKUs) > 0 || len(sub.BonusSKUs) > 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(sub.SubscribeStatus), "UNSUBSCRIBED")
}

type subcriptionDoc struct {
	SubscribeStatus string `json:"subscribe_status"`
	SKUs            []struct {
		SKUCode         string `json:"sku_code"`
		SKUName         string `json:"sku_name"`
		SubscribeStatus string `json:"subscribe_status"`
		EffectiveTime   string `json:"effective_time"`
		ExpiredTime     string `json:"expired_time"`
		Quotas          []struct {
			SKUAttrCode  string `json:"sku_attr_code"`
			QuotaValue   any    `json:"quota_value"`
			SKUValue     any    `json:"sku_value"`
			UsageValue   any    `json:"usage_value"`
			CurrentValue any    `json:"current_value"`
			Remaining    any    `json:"remaining_value"`
			ExpiredTime  string `json:"expired_time"`
		} `json:"quotas"`
	} `json:"skus"`
	BonusSKUs []struct {
		ActivityID    string `json:"activity_id"`
		ActivityName  string `json:"activity_name"`
		CBCResourceID string `json:"cbc_resource_id"`
		Points        any    `json:"points"`
		CurrentValue  any    `json:"current_value"`
		EffectiveTime string `json:"effective_time"`
		ExpiredTime   string `json:"expired_time"`
	} `json:"bonus_skus"`
}

func parseNumber(val any) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		var n float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &n); err == nil {
			return n
		}
	}
	return 0
}

func parseExpiryMs(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func summarizeSubscriptionDoc(sub subcriptionDoc, nowMs int64) accounts.CreditInfo {
	var activeTotal, activeUsed, activeRemaining float64
	var expiredTotal float64
	hasActive := false
	hasAny := false

	windows := make([]accounts.QuotaWindow, 0)
	earliestExpiry := int64(0)

	// 1. Process SKUs
	for _, sku := range sub.SKUs {
		isTrial := strings.HasSuffix(sku.SKUCode, ".trial")
		tier := tierName(sku.SKUCode)
		if sku.SKUName != "" {
			tier = sku.SKUName
		}
		for _, q := range sku.Quotas {
			if q.SKUAttrCode != "officeace_points" {
				continue
			}
			hasAny = true
			total := parseNumber(q.SKUValue)
			if total == 0 {
				total = parseNumber(q.QuotaValue)
			}
			used := parseNumber(q.CurrentValue)
			if used == 0 && q.UsageValue != nil {
				used = parseNumber(q.UsageValue)
			}
			remain := parseNumber(q.Remaining)
			if remain == 0 && total > 0 && used >= 0 {
				remain = math.Max(0, total-used)
			}
			expMs := parseExpiryMs(q.ExpiredTime)
			if expMs == 0 && sku.ExpiredTime != "" {
				expMs = parseExpiryMs(sku.ExpiredTime)
			}

			isExpired := !isTrial && expMs > 0 && expMs <= nowMs

			if isExpired {
				expiredTotal += total
			} else {
				hasActive = true
				activeTotal += total
				activeUsed += used
				activeRemaining += remain
				if expMs > 0 && (earliestExpiry == 0 || expMs < earliestExpiry) {
					earliestExpiry = expMs
				}
			}

			w := accounts.QuotaWindow{
				ID:        "sku-" + sku.SKUCode,
				Name:      tier,
				Unit:      "credits",
				Total:     ptr(round2(total)),
				Used:      ptr(round2(used)),
				Remaining: ptr(round2(remain)),
			}
			if total > 0 {
				w.Utilization = ptr(round2(used / total * 100))
			}
			if expMs > 0 {
				w.ExpiresAt = expMs / 1000
			}
			windows = append(windows, w)
		}
	}

	// 2. Process Bonus SKUs
	for i, b := range sub.BonusSKUs {
		hasAny = true
		points := parseNumber(b.Points)
		used := parseNumber(b.CurrentValue) // OfficeAce: current_value is the consumed amount
		remain := math.Max(0, points-used)  // OfficeAce: remaining = max(0, points - used)
		expMs := parseExpiryMs(b.ExpiredTime)
		isExpired := expMs > 0 && expMs <= nowMs

		if isExpired {
			expiredTotal += points
		} else {
			hasActive = true
			activeTotal += points
			activeUsed += used
			activeRemaining += remain
			if expMs > 0 && (earliestExpiry == 0 || expMs < earliestExpiry) {
				earliestExpiry = expMs
			}
		}

		name := b.ActivityName
		if name == "" {
			name = b.ActivityID
		}
		if name == "" {
			name = "活动奖励"
		}
		w := accounts.QuotaWindow{
			ID:        fmt.Sprintf("bonus-%d", i),
			Name:      name,
			Unit:      "credits",
			Total:     ptr(round2(points)),
			Used:      ptr(round2(used)),
			Remaining: ptr(round2(remain)),
		}
		if points > 0 {
			w.Utilization = ptr(round2(used / points * 100))
		}
		if expMs > 0 {
			w.ExpiresAt = expMs / 1000
		}
		windows = append(windows, w)
	}

	// 3. Primary Aggregate Window
	var aggWindow accounts.QuotaWindow
	if hasActive {
		aggWindow = accounts.QuotaWindow{
			ID:        "total",
			Name:      "总积分",
			Unit:      "credits",
			Total:     ptr(round2(activeTotal)),
			Used:      ptr(round2(activeUsed)),
			Remaining: ptr(round2(activeRemaining)),
		}
		if activeTotal > 0 {
			aggWindow.Utilization = ptr(round2(activeUsed / activeTotal * 100))
		}
		if earliestExpiry > 0 {
			aggWindow.ExpiresAt = earliestExpiry / 1000
		}
	} else if hasAny {
		// All buckets expired: emit exhausted aggregate window so admission/routing pauses
		aggWindow = accounts.QuotaWindow{
			ID:          "total",
			Name:        "总积分（已到期）",
			Unit:        "credits",
			Total:       ptr(round2(expiredTotal)),
			Used:        ptr(round2(expiredTotal)),
			Remaining:   ptr(0.0),
			Utilization: ptr(100.0),
		}
	}

	allWindows := make([]accounts.QuotaWindow, 0, len(windows)+1)
	if aggWindow.Total != nil {
		allWindows = append(allWindows, aggWindow)
	}
	allWindows = append(allWindows, windows...)

	info := accounts.CreditInfo{
		Kind:        "credits",
		Unit:        "credits",
		PackageName: packageLabel(sub),
		Status:      "active",
		UpdatedAt:   time.Now().Unix(),
		Available:   hasAny,
		Windows:     allWindows,
	}
	if hasActive {
		info.Total = round2(activeTotal)
		info.Used = round2(activeUsed)
		info.Remaining = round2(activeRemaining)
		if activeTotal > 0 {
			info.Percent = round2(activeUsed / activeTotal * 100)
		}
	} else if hasAny {
		info.Total = round2(expiredTotal)
		info.Used = round2(expiredTotal)
		info.Remaining = 0
		info.Percent = 100
	} else {
		info.Reason = "上游未返回可用积分配额"
	}

	return info
}

func ptr(v float64) *float64 {
	return &v
}

// packageLabel prefers the plan name the cloud published over a generic guess:
// the same integration serves trial, personal and team plans.
func packageLabel(sub subcriptionDoc) string {
	for _, sku := range sub.SKUs {
		if name := strings.TrimSpace(sku.SKUName); name != "" {
			return "云果办 · " + name
		}
	}
	return "云果办个人套餐"
}

func tierName(skuCode string) string {
	parts := strings.Split(skuCode, ".")
	last := parts[len(parts)-1]
	switch last {
	case "trial":
		return "试用版"
	case "starter":
		return "入门版"
	case "standard":
		return "标准版"
	case "advanced":
		return "高级版"
	case "premium":
		return "尊享版"
	case "ultimate":
		return "旗舰版"
	default:
		if last != "" {
			return last
		}
		return "套餐额度"
	}
}

// DailyStatus checks whether today's bonus has already been claimed.
func (c *Client) DailyStatus(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	norm, _, err := c.Normalize(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	var cred Credentials
	if err := json.Unmarshal(norm, &cred); err != nil {
		return provider.RewardResult{}, err
	}

	sub, err := c.fetchSubscription(ctx, cred)
	if err != nil {
		return provider.RewardResult{}, err
	}

	// An account without a subscription has no bonus pool to claim from. Report
	// that plainly instead of advertising a check-in that would always fail.
	if isUnsubscribed(sub) {
		return provider.RewardResult{State: "unavailable", Message: UnsubscribedReason}, nil
	}

	// The bucket only exists after a claim, so its presence today is the state.
	// The OfficeAce client and this gateway redeem the same campaign: a claim
	// made in the client must read as claimed here, not as a pending action.
	if points, activity, ok := dailyBonus(sub, time.Now()); ok {
		return provider.RewardResult{
			State:   "already_claimed",
			Message: fmt.Sprintf("今日「%s」已领取 %g 积分", activity, points),
		}, nil
	}

	return provider.RewardResult{
		State:   "claimable",
		Message: "OfficeAce 每日签到（可领取赠送积分）",
	}, nil
}

// ClaimDaily calls POST {SUB}/v1/subscription/bonus/claim.
// ClaimDaily redeems today's check-in bucket.
//
// The campaign issues one bucket per day and this gateway is not the only
// client that can redeem it, so the state is read before posting: a bucket
// stamped today already exists when the check-in was made in the OfficeAce
// client, and posting again would only earn an upstream rejection the operator
// would read as a hard failure. The same read repeats after a rejection, which
// also covers a claim made between the first read and the post.
func (c *Client) ClaimDaily(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	norm, _, err := c.Normalize(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	var cred Credentials
	if err := json.Unmarshal(norm, &cred); err != nil {
		return provider.RewardResult{}, err
	}

	claimed := func() (provider.RewardResult, bool) {
		sub, readErr := c.fetchSubscription(ctx, cred)
		if readErr != nil {
			return provider.RewardResult{}, false
		}
		if isUnsubscribed(sub) {
			return provider.RewardResult{State: "unavailable", Message: UnsubscribedReason}, true
		}
		points, activity, ok := dailyBonus(sub, time.Now())
		if !ok {
			return provider.RewardResult{}, false
		}
		return provider.RewardResult{
			State:   "already_claimed",
			Message: fmt.Sprintf("今日「%s」已领取 %g 积分，无需重复领取", activity, points),
		}, true
	}

	if result, ok := claimed(); ok {
		return result, nil
	}

	claimURL := c.SubBase + BonusClaimPath
	headers, err := signV11(cred.AccessKeyID, cred.SecretAccessKey, cred.SecurityToken, cred.ProjectID, http.MethodPost, claimURL, []byte("{}"), map[string]string{
		"content-type": "application/json;charset=utf8",
	}, "")
	if err != nil {
		return provider.RewardResult{}, fmt.Errorf("生成签到签名失败：%w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claimURL, strings.NewReader("{}"))
	if err != nil {
		return provider.RewardResult{}, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return provider.RewardResult{}, ctx.Err()
		}
		return provider.RewardResult{}, provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		bodyBytes = nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The bucket may already exist: trust the campaign state over the status
		// code rather than report a failure the operator cannot act on.
		if result, ok := claimed(); ok {
			return result, nil
		}
		message := fmt.Sprintf("签到请求失败（HTTP %d）", resp.StatusCode)
		if detail := errorDetail(bodyBytes); detail != "" {
			message += "：" + detail
		}
		return provider.RewardResult{State: "failed", Message: message}, providerhttp.Failure("OfficeAce", resp.StatusCode)
	}

	if len(bodyBytes) == 0 {
		return provider.RewardResult{State: "claimed", Message: "已领取（上游响应为空）"}, nil
	}

	var result struct {
		BonusSKUs []struct {
			ActivityName string `json:"activity_name"`
			Points       any    `json:"points"`
		} `json:"bonus_skus"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return provider.RewardResult{State: "already_claimed", Message: "今日已领或已处理"}, nil
	}

	if len(result.BonusSKUs) == 0 {
		return provider.RewardResult{State: "already_claimed", Message: "今日已无新增可领积分"}, nil
	}

	var totalPoints float64
	var names []string
	for _, b := range result.BonusSKUs {
		points := parseNumber(b.Points)
		totalPoints += points
		name := b.ActivityName
		if name == "" {
			name = "活动奖励"
		}
		names = append(names, fmt.Sprintf("%s +%g", name, points))
	}

	msg := strings.Join(names, "；")
	if totalPoints > 0 {
		return provider.RewardResult{
			State:   "claimed",
			Message: msg,
			Credits: &totalPoints,
		}, nil
	}
	return provider.RewardResult{
		State:   "claimed",
		Message: msg,
	}, nil
}
