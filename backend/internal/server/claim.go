package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/jobs"
)

// 每日自动领取运营活动福利。
//
// 「签到」原本包含两块：免费福利 Token（`/api/v1/benefit/claim`）与运营活动奖励
// （`/v1/ops/delivery` + `/v1/ops/claim`）。2026-10 免费权益通道整体下线
// （上游 `benefit-gateway-config` 返回 `enabled:false`，相关接口一律报
// `4000: invalid channel, group not found`），因此这里只剩运营活动。
//
// 线上仍有「每日签到领1000积分」（campaignId=1）等 campaign，按本地日期各领一次。
//
// **只领服务端说能领的**：邀请奖励（推荐码上有礼）与学生认证奖励必须由
// 真实事件触发（好友真实注册 / 真实学生认证），服务端会直接拒绝直接领取，
// 网关也绝不会伪造触发事件。

// claimTickInterval 是领取检查的间隔。
//
// 用「定时轮询 + 按本地日期去重」而不是「算准时刻触发一次」，
// 这样可以避开休眠唤醒、时区切换、进程重启导致的漏领。
const claimTickInterval = 5 * time.Minute

// StartClaimLoop 启动后台自动领取。
func (s *Server) StartClaimLoop(ctx context.Context) {
	go func() {
		// 启动后先延迟几秒，等账号导入与模型刷新完成
		select {
		case <-ctx.Done():
			return
		case <-time.After(8 * time.Second):
		}
		s.RunDailyClaim(ctx)

		ticker := time.NewTicker(claimTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RunDailyClaim(ctx)
			}
		}
	}()
}

// RunDailyClaim 对尚未领取过的账号各领一次当日活动福利。
func (s *Server) RunDailyClaim(ctx context.Context) { s.Rewards.RunDaily(ctx, s.rewardOperations()) }
func (s *Server) ClaimForAccount(ctx context.Context, a *accounts.Account, today string) (string, error) {
	return s.Rewards.Claim(ctx, a, today, s.rewardOperations())
}
func (s *Server) rewardOperations() jobs.RewardOperations {
	return jobs.RewardOperations{
		Native: func(ctx context.Context, a *accounts.Account) (string, error) {
			return s.CodeArts.ClaimWelfare(ctx, s.Cred, a, s.resolveOptions())
		},
		Provider: s.providerCredentials, Registry: s.Providers, DescribeError: codearts.DescribeClaimError,
	}
}

// WelfareSnapshot 返回某账号的活动福利清单（供控制台展示）。
func (s *Server) WelfareSnapshot(ctx context.Context, a *accounts.Account) (*codearts.WelfareDelivery, error) {
	sess, err := s.Cred.Session(ctx, a, s.resolveOptions())
	if err != nil {
		return nil, err
	}
	return s.Up.FetchWelfareDelivery(ctx, sess)
}

// handleAdminWelfare 返回运营活动清单（「活动福利」）。
//
// 活动清单是**按账号**查询的（每个账号的领取状态不同），所以支持用 ?id= 指定；
// 不指定时回退到调度池里的下一个账号。响应里带上账号 id/名称，
// 控制台据此显示「当前查看的是哪个号」，避免用户误以为只有某一个号能领。
func (s *Server) handleAdminWelfare(w http.ResponseWriter, r *http.Request) {
	var acct *accounts.Account
	if id := r.URL.Query().Get("id"); id != "" {
		acct = s.Store.Account(id)
	}
	if acct == nil {
		acct = s.Store.NextAccount()
	}
	if acct == nil {
		for _, a := range s.Store.Accounts() {
			if a.Enabled && s.dailyRewardsSupported(a) {
				acct = a
				break
			}
		}
	}
	if acct == nil {
		writeErr(w, http.StatusBadRequest, apierr.NoAccount.Error(), "invalid_request_error")
		return
	}
	if acct.ProviderID() != "codearts" {
		s.handleProviderWelfare(w, r, acct)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	delivery, err := s.WelfareSnapshot(ctx, acct)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "获取活动列表失败: "+err.Error(), "upstream_error")
		return
	}

	items := make([]map[string]any, 0, len(delivery.Campaigns))
	claimable := 0
	for _, c := range delivery.Campaigns {
		if c.Claimable {
			claimable++
		}
		items = append(items, map[string]any{
			"campaign_id":    c.CampaignID,
			"title":          c.Title,
			"type":           c.Type,
			"status":         c.Status,
			"claimable":      c.Claimable,
			"auto_claimable": c.IsAutoClaimable(),
			"benefit_amount": c.BenefitAmount,
			"benefit_unit":   c.BenefitUnit,
			"description":    c.Description,
			"trigger_mode":   c.Extra.TriggerMode,
			"trigger_event":  c.Extra.TriggerEvent,
			"start_time":     c.Extra.StartTime,
			"end_time":       c.Extra.EndTime,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"account": acct.DisplayName(),
		"id":      acct.ID,
		// 让前端能把账号下拉框列全，而不是只看到当前这一个
		"accounts":        s.welfareAccountOptions(),
		"campaigns":       items,
		"claimable_count": claimable,
	})
}

// welfareAccountOptions 返回可用于查询活动清单的账号列表。
func (s *Server) welfareAccountOptions() []map[string]any {
	accts := s.Store.Accounts()
	out := make([]map[string]any, 0, len(accts))
	for _, a := range accts {
		if !s.dailyRewardsSupported(a) {
			continue
		}
		out = append(out, map[string]any{
			"id": a.ID, "name": a.DisplayName(), "provider": a.ProviderID(),
			"enabled": a.Enabled, "status": a.Status,
			"permanent": a.Session != nil && a.Session.IsPermanent(),
		})
	}
	return out
}

// handleAdminInvite 生成（或取回）专属邀请码与邀请链接。
//
// 只负责把链接取出来给用户 —— 邀请奖励必须由真实好友通过链接完成注册才会
// 产生，服务端对直接领取邀请活动会返回「需要先通过触发接口激活」。
func (s *Server) handleAdminInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		CampaignID int    `json:"campaign_id"`
		ID         string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.CampaignID == 0 {
		body.CampaignID = 3 // 「推荐码上有礼」
	}

	var acct *accounts.Account
	if body.ID != "" {
		acct = s.Store.Account(body.ID)
	}
	if acct == nil {
		acct = s.Store.NextAccount()
	}
	if acct == nil {
		writeErr(w, http.StatusBadRequest, apierr.NoAccount.Error(), "invalid_request_error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	sess, err := s.Cred.Session(ctx, acct, s.resolveOptions())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}
	code, err := s.Up.GenerateInviteCode(ctx, sess, body.CampaignID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "生成邀请链接失败: "+err.Error(), "upstream_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"account":          acct.DisplayName(),
		"campaign_id":      code.CampaignID,
		"invite_code":      code.InviteCode,
		"invite_url":       code.InviteURL,
		"valid":            code.Valid,
		"status":           code.Status,
		"activation_count": code.ActivationCount,
		"max_activations":  code.MaxActivations,
		"hint":             "奖励需好友通过该链接完成注册后才会到账；直接领取邀请活动会被服务端拒绝",
	})
}
