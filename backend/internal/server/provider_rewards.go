package server

import (
	"context"
	"net/http"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/jobs"
	"codearts2api/internal/provider"
)

func claimDateFor(a *accounts.Account, now time.Time) string { return jobs.DateFor(a, now) }
func (s *Server) dailyRewardsSupported(a *accounts.Account) bool {
	return jobs.Supported(a, s.Providers)
}
func (s *Server) providerDailyStatus(ctx context.Context, a *accounts.Account) (provider.RewardResult, error) {
	return s.Rewards.Status(ctx, a, s.rewardOperations())
}
func (s *Server) handleProviderWelfare(w http.ResponseWriter, r *http.Request, a *accounts.Account) {
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
	if a.ProviderID() == provider.TeleAgent {
		item["title"] = "TeleAgent 每日积分"
		item["auto_claimable"] = result.State == "claimable"
	}
	if a.ProviderID() == provider.Qoder {
		item["title"] = "Qoder 活动积分"
		item["benefit_unit"] = "Credits"
		item["auto_claimable"] = result.State == "claimable"
	}
	campaigns := []any{item}
	for _, notice := range result.Notices {
		campaigns = append(campaigns, map[string]any{"title": notice.Title, "type": "activity_notice", "status": "ENTRY", "claimable": false, "auto_claimable": false, "description": notice.Description, "trigger_event": notice.Description})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": a.ID, "account": a.DisplayName(), "provider": a.ProviderID(), "accounts": s.welfareAccountOptions(), "campaigns": campaigns, "claimable_count": count, "message": result.Message, "daily_status": result.State, "next_claim_at": a.NextClaimAt})
}
