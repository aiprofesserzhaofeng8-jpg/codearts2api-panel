package server

import (
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"context"
	"fmt"
	"net/http"
	"time"
)

var rewardLocation = time.FixedZone("UTC+8", 8*60*60)

func claimDateFor(a *codearts.Account, now time.Time) string {
	if a.ProviderID() != provider.CodeArts {
		return now.In(rewardLocation).Format("2006-01-02")
	}
	return now.Format("2006-01-02")
}
func (s *Server) dailyRewardsSupported(a *codearts.Account) bool {
	if a.ProviderID() == provider.CodeArts {
		return true
	}
	if s.Providers == nil {
		return false
	}
	adapter, ok := s.Providers.Get(a.ProviderID())
	if !ok {
		return false
	}
	_, ok = adapter.(provider.DailyRewards)
	return ok
}
func (s *Server) providerDailyStatus(ctx context.Context, a *codearts.Account) (provider.RewardResult, error) {
	today := claimDateFor(a, time.Now())
	if a.LastClaimDate == today {
		return provider.RewardResult{State: "already_claimed", Message: "今日签到已在本网关确认"}, nil
	}
	adapter, raw, err := s.providerCredentials(ctx, a)
	if err != nil {
		return provider.RewardResult{}, err
	}
	rewards, ok := adapter.(provider.DailyRewards)
	if !ok {
		return provider.RewardResult{}, fmt.Errorf("此上游每日签到尚未接入")
	}
	result, err := rewards.DailyStatus(ctx, raw)
	if err == nil && result.Confirmed() {
		if saveErr := s.Store.SaveDailyReward(a.ID, today, result.State, result.Message, true, 0); saveErr != nil {
			return result, fmt.Errorf("上游已确认签到，但本地状态保存失败")
		}
	}
	return result, err
}
func (s *Server) claimProviderDaily(ctx context.Context, a *codearts.Account, today string) (string, error) {
	release, err := s.rewardRuntime.lock(ctx, a.ID)
	if err != nil {
		return "", err
	}
	defer release()
	latest := s.Store.Account(a.ID)
	if latest == nil {
		return "", fmt.Errorf("账号已删除")
	}
	if !latest.Enabled {
		return "", fmt.Errorf("账号已停用，请启用后再领取")
	}
	if latest.LastClaimDate == today {
		return "今日已确认签到，无需重复领取", nil
	}
	if latest.NextClaimAt > time.Now().Unix() {
		return "", fmt.Errorf("该账号签到暂处于等待期，请在 %s 后重试", time.Unix(latest.NextClaimAt, 0).In(rewardLocation).Format("15:04:05"))
	}
	adapter, raw, err := s.providerCredentials(ctx, latest)
	result := provider.RewardResult{}
	if err == nil {
		rewards, ok := adapter.(provider.DailyRewards)
		if !ok {
			return "", fmt.Errorf("此上游每日签到尚未接入")
		}
		result, err = rewards.ClaimDaily(ctx, raw)
	}
	state, message, next := "failed", "", time.Now().Add(30*time.Minute).Unix()
	confirmed := false
	if err != nil {
		message = err.Error()
	} else {
		state = result.State
		message = result.Message
		confirmed = result.Confirmed()
		if confirmed {
			next = 0
		}
	}
	if saveErr := s.Store.SaveDailyReward(a.ID, today, state, message, confirmed, next); saveErr != nil {
		return message, fmt.Errorf("签到结果保存失败；请先核对上游状态，避免重复提交")
	}
	s.creditMu.Lock()
	delete(s.creditCache, a.ID)
	s.creditMu.Unlock()
	if err != nil {
		return message, err
	}
	if !confirmed {
		return message, fmt.Errorf("%s", message)
	}
	return message, nil
}
func (s *Server) handleProviderWelfare(w http.ResponseWriter, r *http.Request, a *codearts.Account) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.providerDailyStatus(ctx, a)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	count := 0
	if result.State == "claimable" {
		count = 1
	}
	item := map[string]any{"campaign_id": 0, "title": "每日签到", "type": "daily_checkin", "status": result.State, "claimable": result.State == "claimable", "auto_claimable": result.State == "claimable" || result.State == "unknown", "benefit_unit": "积分", "description": result.Message}
	if result.Credits != nil {
		item["benefit_amount"] = *result.Credits
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": a.ID, "account": a.DisplayName(), "provider": a.ProviderID(), "accounts": s.welfareAccountOptions(), "campaigns": []any{item}, "claimable_count": count, "message": result.Message, "daily_status": result.State, "next_claim_at": a.NextClaimAt})
}
