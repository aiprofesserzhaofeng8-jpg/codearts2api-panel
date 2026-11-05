package teleagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const DailyStatusPath = "/superCowork/sapi/user/portal/companyStore/v2/checkDayTask"
const DailyClaimPath = "/superCowork/sapi/user/portal/companyStore/checkTask?type=daily_login"
const PortalDailyStatusPath = "/meter/super-agent/user/portal/companyStore/v2/checkDayTask"
const PortalDailyClaimPath = "/meter/super-agent/user/portal/companyStore/checkTask?type=daily_login"

func portalHeaders(a Credentials) http.Header {
	h := http.Header{"X-Token": {a.OperationToken}, "Userid": {a.AccountID}, "Accept": {"application/json"}}
	cookie := "operationToken=" + a.OperationToken
	if a.OperToken != "" {
		cookie += "; operToken=" + a.OperToken
	}
	h.Set("Cookie", cookie)
	return h
}

func rewardRequest(a Credentials, claim bool) (string, http.Header) {
	path, headers := DailyStatusPath, identityHeaders(a)
	if claim {
		path = DailyClaimPath
	}
	if a.OperationToken != "" {
		path, headers = PortalDailyStatusPath, portalHeaders(a)
		if claim {
			path = PortalDailyClaimPath
		}
	}
	// The upstream claim endpoint mutates state despite using GET. Neither status
	// nor mutation may be satisfied by an intermediary's cached response.
	headers.Set("Cache-Control", "no-store")
	headers.Set("Pragma", "no-cache")
	return path, headers
}

type dailyReward struct {
	result provider.RewardResult
	total  *float64
}

func (c *Client) daily(ctx context.Context, a Credentials) (dailyReward, error) {
	path, headers := rewardRequest(a, false)
	resp, err := providerhttp.Do(ctx, c.HTTP, "TeleAgent", http.MethodGet, c.Origin+path, headers, nil)
	if err != nil {
		return dailyReward{}, err
	}
	o, err := unwrap(resp)
	if err != nil {
		return dailyReward{}, err
	}
	if id := accountField(o, "accountId", "account_id", "userId"); id != "" && id != a.AccountID {
		return dailyReward{}, invalid("签到返回身份与当前账号不一致")
	}
	var task map[string]json.RawMessage
	if json.Unmarshal(o["daily_task"], &task) != nil || task == nil {
		return dailyReward{}, providerhttp.Broken("TeleAgent 每日积分状态")
	}
	var claimed *bool
	if json.Unmarshal(task["claimed_today"], &claimed) != nil || claimed == nil {
		return dailyReward{}, providerhttp.Broken("TeleAgent 每日积分状态")
	}
	var visible *bool
	if b, exists := task["visible"]; exists && json.Unmarshal(b, &visible) != nil {
		return dailyReward{}, providerhttp.Broken("TeleAgent 每日积分状态")
	}
	credits, err := accounts.QuotaNumber(task, "daily_claimable_points")
	if err != nil || credits != nil && *credits < 0 {
		return dailyReward{}, providerhttp.Broken("TeleAgent 每日积分状态")
	}
	total, err := accounts.QuotaNumber(task, "total_claimed_points")
	if err != nil || total != nil && *total < 0 {
		return dailyReward{}, providerhttp.Broken("TeleAgent 每日积分状态")
	}
	result := provider.RewardResult{State: "claimable", Message: "TeleAgent 今日可领取每日积分", Credits: credits}
	var activity struct {
		Visible bool   `json:"visible"`
		Title   string `json:"activity_title"`
	}
	if json.Unmarshal(o["activity"], &activity) == nil && activity.Visible && activity.Title != "" && len(activity.Title) <= 512 && providerhttp.Safe(activity.Title) {
		result.Notices = []provider.RewardNotice{{Title: activity.Title, Description: "上游活动提示；请在官方核对参与条件，当前不自动领取此活动"}}
	}
	if *claimed {
		result.State, result.Message = "already_claimed", "TeleAgent 上游确认今日积分已领取"
	} else if visible != nil && !*visible || credits != nil && *credits == 0 {
		result.State, result.Message = "unavailable", "TeleAgent 当前账号未开放每日积分或今日无可领积分"
	} else if credits == nil {
		result.State, result.Message = "unknown", "TeleAgent 未返回完整的每日可领积分，暂不提交领取"
	}
	return dailyReward{result: result, total: total}, nil
}

func (c *Client) DailyStatus(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	a, err := decode(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	status, err := c.daily(ctx, a)
	return status.result, err
}

func (c *Client) ClaimDaily(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	a, err := decode(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	before, err := c.daily(ctx, a)
	if err != nil {
		return provider.RewardResult{}, err
	}
	if before.result.State != "claimable" {
		return before.result, nil
	}
	path, headers := rewardRequest(a, true)
	// Fresh HTTP/1 connections prevent transparent replay of this mutating GET
	// after a reused connection fails. No explicit mutation retry or base fallback.
	client := *c.HTTP
	if transport, ok := c.HTTP.Transport.(*http.Transport); ok {
		tr := transport.Clone()
		tr.DisableKeepAlives = true
		tr.Protocols = new(http.Protocols)
		tr.Protocols.SetHTTP1(true)
		client.Transport = tr
		defer tr.CloseIdleConnections()
	}
	resp, claimErr := providerhttp.Do(ctx, &client, "TeleAgent", http.MethodGet, c.Origin+path, headers, nil)
	if claimErr == nil {
		var envelope map[string]json.RawMessage
		envelope, claimErr = providerhttp.JSON(resp, "TeleAgent")
		if claimErr == nil {
			code := string(envelope["code"])
			if code != "" && code != "0" && code != `"0"` && code != "200" && code != `"200"` {
				claimErr = businessError(envelope["code"])
			}
			if string(envelope["success"]) == "false" {
				claimErr = providerhttp.Broken("TeleAgent")
			}
			// Payloads can be arrays. Only the subsequent daily status confirms receipt.
		}
	}
	if ctx.Err() != nil {
		return provider.RewardResult{}, ctx.Err()
	}
	after, statusErr := c.daily(ctx, a)
	if statusErr == nil && after.result.State == "already_claimed" {
		if claimErr != nil {
			return after.result, nil
		}
		result := provider.RewardResult{State: "claimed", Message: "TeleAgent 每日积分已复查确认，最新余额将自动刷新"}
		if before.total != nil && after.total != nil && *after.total > *before.total {
			amount := *after.total - *before.total
			result.Credits = &amount
			result.Message = fmt.Sprintf("TeleAgent 每日积分已确认领取 %g 积分，最新余额将自动刷新", amount)
		}
		return result, nil
	}
	if claimErr != nil {
		return provider.RewardResult{}, claimErr
	}
	if statusErr != nil {
		return provider.RewardResult{}, fmt.Errorf("TeleAgent 已提交领取但复查失败，未标记已领；请稍后查询状态")
	}
	return provider.RewardResult{State: "unknown", Message: "TeleAgent 已提交领取，但上游尚未确认今日已领；请稍后查询状态"}, nil
}

var _ provider.DailyRewards = (*Client)(nil)
