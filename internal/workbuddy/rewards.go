package workbuddy

import (
	"bytes"
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (c *Client) DailyStatus(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.RewardResult{}, err
	}
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	a, _ := decode(normalized)
	if a.EnterpriseID != "" {
		return provider.RewardResult{State: "unavailable", Message: "当前仅接入 WorkBuddy 个人账号每日签到"}, nil
	}
	return provider.RewardResult{State: "unknown", Message: "WorkBuddy 未提供已核实的独立签到状态接口，可提交每日签到，由上游确认是否已领"}, nil
}
func (c *Client) ClaimDaily(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	a, err := decode(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	if a.EnterpriseID != "" {
		return provider.RewardResult{State: "unavailable", Message: "当前仅接入 WorkBuddy 个人账号每日签到，企业账号请在官方核对"}, nil
	}
	base := c.CNBilling
	paths := []string{"/v2/billing/meter/daily-checkin"}
	if a.Realm == "global" {
		base = c.GlobalBase
		paths = []string{"/billing/meter/daily-checkin", "/v2/billing/meter/daily-checkin"}
	}
	for i, path := range paths {
		req, err := http.NewRequestWithContext(ctx, "POST", base+path, bytes.NewBufferString(`{}`))
		if err != nil {
			return provider.RewardResult{}, fmt.Errorf("WorkBuddy 签到请求无效")
		}
		c.headers(req, a)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return provider.RewardResult{}, fmt.Errorf("WorkBuddy 签到结果未确认，请稍后检查（%T）", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		resp.Body.Close()
		if readErr != nil || len(body) > 1<<20 {
			return provider.RewardResult{}, fmt.Errorf("WorkBuddy 签到响应读取失败，未确认成功")
		}
		if resp.StatusCode == 404 && i+1 < len(paths) {
			continue
		}
		var envelope struct {
			Code *int   `json:"code"`
			Msg  string `json:"msg"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.Code == nil {
			return provider.RewardResult{}, fmt.Errorf("WorkBuddy 签到响应缺少明确业务结果")
		}
		if resp.StatusCode == 200 && *envelope.Code == 0 {
			return provider.RewardResult{State: "claimed", Message: "WorkBuddy 每日签到已由上游确认；奖励数量以上游余额为准"}, nil
		}
		message := strings.ToLower(envelope.Msg)
		already := strings.Contains(message, "已签到") || strings.Contains(message, "已经签到") || strings.Contains(message, "already checked in") || strings.Contains(message, "already check-in") || strings.Contains(message, "already signed in")
		if resp.StatusCode == 200 && *envelope.Code == 14001 && already {
			return provider.RewardResult{State: "already_claimed", Message: "WorkBuddy 上游确认今日已签到，无需重复领取"}, nil
		}
		if err := upstreamError(resp.StatusCode, body); err != nil {
			return provider.RewardResult{}, err
		}
		return provider.RewardResult{}, fmt.Errorf("WorkBuddy 签到结果未确认")
	}
	return provider.RewardResult{}, fmt.Errorf("WorkBuddy 签到端点不可用")
}

var _ provider.DailyRewards = (*Client)(nil)
