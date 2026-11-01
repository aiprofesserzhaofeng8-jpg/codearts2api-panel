package trae

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"fmt"
)

const CheckinStatusPath = "/trae/api/v2/ug/checkin_credits/status"
const CheckinClaimPath = "/trae/api/v2/ug/checkin_credits/claim"

// The check-in source identifies the OAuth client family, not the user's plan.
func checkinBody(a Credentials) map[string]any {
	source := 2 // SOLO CN
	if a.ClientID == IDEClientID {
		source = 1 // Trae CN IDE
	}
	return map[string]any{"req_source": source}
}

func (c *Client) DailyStatus(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	a, err := decode(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	data, err := c.doJSON(ctx, a, "credit", c.CreditBase+CheckinStatusPath, checkinBody(a))
	if err != nil {
		return provider.RewardResult{}, err
	}
	data = unwrapData(data, "checked_in")
	var status struct {
		Enable  *bool    `json:"enable"`
		Checked *bool    `json:"checked_in"`
		Credits *float64 `json:"credits"`
	}
	if json.Unmarshal(data, &status) != nil || status.Enable == nil || status.Checked == nil {
		return provider.RewardResult{}, fmt.Errorf("Trae 未返回完整签到状态，不能确认是否可领")
	}
	out := provider.RewardResult{State: "claimable", Message: "Trae 今日可签到"}
	if status.Credits != nil && *status.Credits > 0 {
		out.Credits = status.Credits
	}
	if *status.Checked {
		out.State = "already_claimed"
		out.Message = "Trae 上游确认今日已签到"
	} else if !*status.Enable {
		out.State = "unavailable"
		out.Message = "Trae 当前账号未开放签到或活动暂不可用"
	}
	return out, nil
}
func (c *Client) ClaimDaily(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	before, err := c.DailyStatus(ctx, raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	if before.State != "claimable" {
		return before, nil
	}
	a, err := decode(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	_, claimErr := c.doJSON(ctx, a, "credit", c.CreditBase+CheckinClaimPath, checkinBody(a))
	// No immediate POST retries, no device-ID rotation. Even a device-scoped
	// already-claimed response does not prove this particular account received it.
	after, statusErr := c.DailyStatus(ctx, raw)
	if statusErr == nil && after.State == "already_claimed" {
		if claimErr != nil {
			return after, nil
		}
		return provider.RewardResult{State: "claimed", Message: "Trae 每日签到已确认，最新额度以余额查询为准", Credits: before.Credits}, nil
	}
	if claimErr != nil {
		return provider.RewardResult{}, claimErr
	}
	if statusErr != nil {
		return provider.RewardResult{}, fmt.Errorf("Trae 已提交签到，但复查失败；未标记为已领，请稍后查询状态")
	}
	return provider.RewardResult{State: "unknown", Message: "Trae 已提交签到，但上游尚未确认该账号已领取，请稍后查询"}, nil
}

var _ provider.DailyRewards = (*Client)(nil)
