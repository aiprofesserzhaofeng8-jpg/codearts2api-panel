// Package jobs owns administrative task coordination, not platform protocols
// or the scheduler's atomic admission and ledger transactions.
package jobs

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type RewardStore interface {
	Accounts() []*accounts.Account
	Account(string) *accounts.Account
	Settings() config.Settings
	Save() error
	MarkClaimResult(string, string, bool, string)
	SaveDailyReward(string, string, string, string, bool, int64) error
}
type RewardOperations struct {
	Native        func(context.Context, *accounts.Account) (string, error)
	Provider      func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error)
	Registry      *provider.Registry
	DescribeError func(error) string
}
type Rewards struct {
	store            RewardStore
	locks            accounts.OperationLocks
	invalidateCredit func(string)
	logf             func(string, ...any)
}

func NewRewards(store RewardStore, invalidateCredit func(string), logf func(string, ...any)) *Rewards {
	return &Rewards{store: store, invalidateCredit: invalidateCredit, logf: logf}
}

var rewardLocation = time.FixedZone("UTC+8", 8*60*60)

func DateFor(a *accounts.Account, now time.Time) string {
	if a.ProviderID() != provider.CodeArts {
		return now.In(rewardLocation).Format("2006-01-02")
	}
	return now.Format("2006-01-02")
}
func Supported(a *accounts.Account, registry *provider.Registry) bool {
	if a.ProviderID() == provider.CodeArts {
		return true
	}
	if registry == nil {
		return false
	}
	adapter, ok := registry.Get(a.ProviderID())
	if !ok {
		return false
	}
	_, ok = adapter.(provider.DailyRewards)
	return ok
}
func (s *Rewards) Status(ctx context.Context, a *accounts.Account, ops RewardOperations) (provider.RewardResult, error) {
	today := DateFor(a, time.Now())
	if a.LastClaimDate == today {
		return provider.RewardResult{State: "already_claimed", Message: "今日签到已在本网关确认"}, nil
	}
	adapter, raw, err := ops.Provider(ctx, a)
	if err != nil {
		return provider.RewardResult{}, err
	}
	rewards, ok := adapter.(provider.DailyRewards)
	if !ok {
		return provider.RewardResult{}, fmt.Errorf("此上游每日签到尚未接入")
	}
	result, err := rewards.DailyStatus(ctx, raw)
	if err == nil && result.Confirmed() {
		if saveErr := s.store.SaveDailyReward(a.ID, today, result.State, result.Message, true, 0); saveErr != nil {
			return result, fmt.Errorf("上游已确认签到，但本地状态保存失败")
		}
	}
	return result, err
}
func (s *Rewards) claimProviderDaily(ctx context.Context, a *accounts.Account, today string, ops RewardOperations) (string, error) {
	release, err := s.locks.Acquire(ctx, a.ID)
	if err != nil {
		return "", err
	}
	defer release()
	latest := s.store.Account(a.ID)
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
	adapter, raw, err := ops.Provider(ctx, latest)
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
	if saveErr := s.store.SaveDailyReward(a.ID, today, state, message, confirmed, next); saveErr != nil {
		return message, fmt.Errorf("签到结果保存失败；请先核对上游状态，避免重复提交")
	}
	s.invalidateCredit(a.ID)
	if err != nil {
		return message, err
	}
	if !confirmed {
		return message, fmt.Errorf("%s", message)
	}
	return message, nil
}
func (s *Rewards) RunDaily(ctx context.Context, ops RewardOperations) {
	st := s.store.Settings()
	if !st.AutoClaim {
		return
	}
	now := time.Now()
	if !WindowOpen(now, st.ClaimAfterHour, st.ClaimAfterMinute) {
		return
	}
	for _, a := range s.store.Accounts() {
		today := DateFor(a, now)
		if !Supported(a, ops.Registry) || !a.Enabled || a.LastClaimDate == today || a.NextClaimAt > now.Unix() {
			continue
		}
		if _, err := s.Claim(ctx, a, today, ops); err != nil {
			s.logf("账号 %s 自动领取活动福利失败: %s", a.DisplayName(), ops.DescribeError(err))
		}
	}
}

// WindowOpen 报告当前是否已过每日领取时刻。
func WindowOpen(now time.Time, hour, minute int) bool {
	cur := now.Hour()*60 + now.Minute()
	return cur >= hour*60+minute
}

// Claim preserves the distinct native and external persistence guarantees.
// CodeArts has no new deduplication lock or retry gate introduced here.
func (s *Rewards) Claim(ctx context.Context, a *accounts.Account, today string, ops RewardOperations) (string, error) {
	if a.ProviderID() != provider.CodeArts {
		return s.claimProviderDaily(ctx, a, DateFor(a, time.Now()), ops)
	}
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}
	msg, err := ops.Native(ctx, a)
	record := msg
	if record == "" && err != nil {
		record = err.Error()
	}
	s.store.MarkClaimResult(a.ID, today, err == nil, record)
	if saveErr := s.store.Save(); saveErr != nil {
		s.logf("保存状态文件失败: %v", saveErr)
	}
	return msg, err
}

// Batch preserves sequential manual claims with one 60s context per account.
// Explicit requests retain their original provider/enablement handling.
func (s *Rewards) Batch(ctx context.Context, targets []*accounts.Account, explicit bool, today string, ops RewardOperations) ([]map[string]any, int) {
	results := make([]map[string]any, 0, len(targets))
	okCount := 0
	for _, a := range targets {
		if !explicit && (!a.Enabled || !Supported(a, ops.Registry)) {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		msg, err := s.Claim(ctx, a, today, ops)
		cancel()
		row := map[string]any{"id": a.ID, "name": a.DisplayName(), "ok": err == nil, "provider": a.ProviderID()}
		if err != nil {
			row["message"] = ops.DescribeError(err)
		} else {
			row["message"] = msg
			okCount++
		}
		results = append(results, row)
	}
	return results, okCount
}
