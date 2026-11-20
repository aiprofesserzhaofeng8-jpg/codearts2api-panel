package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/providerhttp"
)

var quotaDecimal = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)

func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	if a.AuthMode == "api_key" {
		return json.Marshal(accounts.CreditInfo{Kind: "unsupported", Reason: "OpenAI API Key 接入未提供订阅窗口查询，请在官方 API 控制台查看用量与账单"})
	}
	h := headers(a)
	h.Set("OpenAI-Beta", "codex-1")
	h.Set("Accept", "application/json")
	resp, e := providerhttp.Do(ctx, c.HTTP, "ChatGPT", http.MethodGet, c.UsageURL, h, nil)
	if e != nil {
		return nil, e
	}
	o, e := providerhttp.JSON(resp, "ChatGPT")
	if e != nil {
		return nil, e
	}
	return parseQuota(o, time.Now().Unix())
}
func parseQuota(o map[string]json.RawMessage, now int64) (json.RawMessage, error) {
	info := accounts.CreditInfo{Kind: "windows", Unit: "credits", UpdatedAt: now, PackageName: providerhttp.Field(o, "plan_type")}
	appendLimit := func(raw json.RawMessage, id, prefix string) error {
		if len(raw) == 0 || string(raw) == "null" {
			return nil
		}
		var limit map[string]json.RawMessage
		if json.Unmarshal(raw, &limit) != nil || limit == nil {
			return providerhttp.Broken("ChatGPT")
		}
		for _, slot := range []string{"primary_window", "secondary_window"} {
			b := limit[slot]
			if len(b) == 0 || string(b) == "null" {
				continue
			}
			var v struct {
				Percent *float64        `json:"used_percent"`
				Seconds int64           `json:"limit_window_seconds"`
				Reset   json.RawMessage `json:"reset_at"`
				After   *int64          `json:"reset_after_seconds"`
			}
			if json.Unmarshal(b, &v) != nil {
				return providerhttp.Broken("ChatGPT")
			}
			if v.Percent == nil {
				continue
			}
			name := "使用窗口"
			switch v.Seconds {
			case 18000:
				name = "5 小时窗口"
			case 604800:
				name = "7 天窗口"
			default:
				if v.Seconds > 0 && v.Seconds%3600 == 0 {
					name = fmt.Sprintf("%d 小时窗口", v.Seconds/3600)
				} else if v.Seconds > 0 && v.Seconds%60 == 0 {
					name = fmt.Sprintf("%d 分钟窗口", v.Seconds/60)
				}
			}
			if prefix != "" {
				name = prefix + " · " + name
			}
			w := accounts.QuotaWindow{ID: id + "_" + slot, Name: name, Unit: "percent", Utilization: v.Percent, WindowSeconds: v.Seconds, ResetsAt: providerhttp.Timestamp(v.Reset)}
			if w.ResetsAt == 0 && v.After != nil {
				if *v.After < 0 || *v.After > 366*86400 {
					return providerhttp.Broken("ChatGPT")
				}
				w.ResetsAt = now + *v.After
			}
			if !w.Complete() {
				return providerhttp.Broken("ChatGPT")
			}
			info.Windows = append(info.Windows, w)
		}
		return nil
	}
	if e := appendLimit(o["rate_limit"], "codex", ""); e != nil {
		return nil, e
	}
	if b := o["additional_rate_limits"]; len(b) > 0 && string(b) != "null" {
		var limits []map[string]json.RawMessage
		if json.Unmarshal(b, &limits) != nil || len(limits) > 32 {
			return nil, providerhttp.Broken("ChatGPT")
		}
		for index, limit := range limits {
			name := providerhttp.Field(limit, "limit_name", "metered_feature")
			if len(name) > 120 || strings.ContainsAny(name, "\r\n\x00") {
				return nil, providerhttp.Broken("ChatGPT")
			}
			if name == "" {
				name = "附加额度"
			}
			if e := appendLimit(limit["rate_limit"], fmt.Sprintf("additional_%d", index), name); e != nil {
				return nil, e
			}
		}
	}
	if b := o["credits"]; len(b) > 0 && string(b) != "null" {
		var balance struct {
			Amount    *string `json:"balance"`
			Unlimited bool    `json:"unlimited"`
		}
		if json.Unmarshal(b, &balance) != nil {
			return nil, providerhttp.Broken("ChatGPT")
		}
		if balance.Amount != nil && (len(*balance.Amount) > 80 || !quotaDecimal.MatchString(*balance.Amount)) {
			return nil, providerhttp.Broken("ChatGPT")
		}
		if balance.Amount != nil || balance.Unlimited {
			info.Balance = &accounts.QuotaBalance{Amount: balance.Amount, Unlimited: balance.Unlimited, Unit: "credits"}
		}
	}
	info.Available = len(info.Windows) > 0 || info.Balance != nil
	if !info.Available {
		info.Reason = "上游未返回订阅窗口或 Credits 余额，无法推断剩余额度"
	}
	return json.Marshal(info)
}
