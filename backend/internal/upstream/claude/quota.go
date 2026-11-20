package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
)

func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	if a.AuthMode == "api_key" {
		return json.Marshal(accounts.CreditInfo{Kind: "unsupported", Reason: "Anthropic API Key 接入未提供订阅窗口查询，请在官方 API 控制台查看用量与账单"})
	}
	resp, e := providerhttp.Do(ctx, c.HTTP, "Claude", http.MethodGet, c.API+"/api/oauth/usage", headers(a), nil)
	if e != nil {
		return nil, e
	}
	o, e := providerhttp.JSON(resp, "Claude")
	if e != nil {
		return nil, e
	}
	return parseQuota(o, time.Now().Unix())
}
func parseQuota(o map[string]json.RawMessage, now int64) (json.RawMessage, error) {
	info := accounts.CreditInfo{Kind: "windows", UpdatedAt: now}
	for _, def := range []struct {
		ID, Name string
		Seconds  int64
	}{{"five_hour", "5 小时窗口", 18000}, {"seven_day", "7 天窗口", 604800}, {"seven_day_sonnet", "Sonnet · 7 天窗口", 604800}, {"seven_day_opus", "Opus · 7 天窗口", 604800}, {"seven_day_overage_included", "额外用量 · 7 天窗口", 604800}, {"seven_day_oauth_apps", "OAuth 应用 · 7 天窗口", 604800}} {
		b := o[def.ID]
		if len(b) == 0 || string(b) == "null" {
			continue
		}
		var v struct {
			Utilization *float64        `json:"utilization"`
			Reset       json.RawMessage `json:"resets_at"`
		}
		if json.Unmarshal(b, &v) != nil {
			return nil, providerhttp.Broken("Claude")
		}
		if v.Utilization == nil {
			continue
		}
		w := accounts.QuotaWindow{ID: def.ID, Name: def.Name, Unit: "percent", Utilization: v.Utilization, ResetsAt: providerhttp.Timestamp(v.Reset), WindowSeconds: def.Seconds}
		if !w.Complete() {
			return nil, providerhttp.Broken("Claude")
		}
		info.Windows = append(info.Windows, w)
	}
	info.Available = len(info.Windows) > 0
	if !info.Available {
		info.Reason = "上游未返回订阅使用窗口，无法推断剩余额度"
	}
	return json.Marshal(info)
}
