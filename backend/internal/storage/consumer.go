package storage

import (
	"crypto/rand"
	"fmt"
	"slices"
	"strings"
	"time"

	"codearts2api/internal/access"
)

// All entitlements use Store's existing transaction lock and strict snapshot.
// No second authorization cache, worker, or lock domain is introduced.
func (s *Store) consumerTransaction(fn func() error) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.st.Consumer
	before.Ledger = slices.Clone(before.Ledger)
	before.Plans = slices.Clone(before.Plans)
	before.Redemptions = slices.Clone(before.Redemptions)
	before.Identities = slices.Clone(before.Identities)
	users := slices.Clone(s.st.Users)
	groups := make([]string, len(s.st.Keys))
	for i, k := range s.st.Keys {
		groups[i] = k.GroupID
	}
	err := fn()
	if err == nil {
		err = s.saveGroupStateLocked()
	}
	if err != nil {
		s.st.Consumer = before
		s.st.Users = users
		for i, k := range s.st.Keys {
			k.GroupID = groups[i]
		}
	}
	return err
}
func (s *Store) idleConsumerLocked(id string) (*access.User, error) {
	u := s.userLocked(id)
	if u == nil {
		return nil, fmt.Errorf("用户不存在")
	}
	if s.budgetActive[userBudgetID(id)] > 0 {
		return nil, fmt.Errorf("用户仍有在途请求，请完成后再修改权益")
	}
	return u, nil
}
func (s *Store) ledgerLocked(u *access.User, delta int64, kind, ref, note string) {
	s.st.Consumer.Ledger = append(s.st.Consumer.Ledger, access.LedgerEntry{ID: rand.Text(), UserID: u.ID, Time: time.Now().Unix(), Delta: delta, Balance: u.Wallet.Balance, Kind: kind, Reference: ref, Note: note})
	// The UI explicitly exposes the retention boundary; cumulative balance is independent.
	if len(s.st.Consumer.Ledger) > 10000 {
		s.st.Consumer.Ledger = s.st.Consumer.Ledger[len(s.st.Consumer.Ledger)-10000:]
	}
}
func (s *Store) AdjustWallet(id string, expected uint64, delta int64, enabled, clearReview bool, note string) error {
	if delta < -access.MaxCredits || delta > access.MaxCredits || strings.TrimSpace(note) == "" || len(note) > 500 {
		return fmt.Errorf("额度或操作原因无效")
	}
	return s.consumerTransaction(func() error {
		u, err := s.idleConsumerLocked(id)
		if err != nil {
			return err
		}
		if u.Wallet.Revision != expected {
			return fmt.Errorf("额度已变更，请刷新后重试，避免重复发放")
		}
		if u.Wallet.Balance+delta < 0 || u.Wallet.Balance+delta > access.MaxCredits {
			return fmt.Errorf("调整后额度超出允许范围")
		}
		u.Wallet.Balance += delta
		u.Wallet.Enabled = enabled
		u.Wallet.Revision++
		if clearReview {
			u.Wallet.ReviewRequired = false
		}
		s.ledgerLocked(u, delta, "admin", "", note)
		return nil
	})
}
func (s *Store) UserLedger(id string) []access.LedgerEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []access.LedgerEntry{}
	for i := len(s.st.Consumer.Ledger) - 1; i >= 0; i-- {
		e := s.st.Consumer.Ledger[i]
		if e.UserID == id {
			out = append(out, e)
		}
	}
	return out
}
func (s *Store) Plans() []access.Plan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]access.Plan{}, s.st.Consumer.Plans...)
}
func (s *Store) planLocked(id string) *access.Plan {
	for i := range s.st.Consumer.Plans {
		if s.st.Consumer.Plans[i].ID == id {
			return &s.st.Consumer.Plans[i]
		}
	}
	return nil
}

type PlanReferences struct {
	Subscriptions int `json:"subscriptions"`
	Codes         int `json:"codes"`
}

func (s *Store) planReferencesLocked() map[string]PlanReferences {
	refs := make(map[string]PlanReferences)
	for _, u := range s.st.Users {
		if u.Subscription.ID != "" {
			id := u.Subscription.Plan.ID
			r := refs[id]
			r.Subscriptions++
			refs[id] = r
		}
	}
	for _, c := range s.st.Consumer.Redemptions {
		if c.Kind == "subscription" {
			r := refs[c.PlanID]
			r.Codes++
			refs[c.PlanID] = r
		}
	}
	return refs
}

func (s *Store) PlanCatalog() ([]access.Plan, map[string]PlanReferences) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]access.Plan{}, s.st.Consumer.Plans...), s.planReferencesLocked()
}

func (s *Store) DeletePlan(id string) error {
	return s.consumerTransaction(func() error {
		i := slices.IndexFunc(s.st.Consumer.Plans, func(p access.Plan) bool { return p.ID == id })
		if i < 0 {
			return fmt.Errorf("套餐不存在")
		}
		if r := s.planReferencesLocked()[id]; r != (PlanReferences{}) {
			return fmt.Errorf("不能删除：仍关联 %d 个订阅、%d 张兑换码", r.Subscriptions, r.Codes)
		}
		s.st.Consumer.Plans = slices.Delete(s.st.Consumer.Plans, i, i+1)
		return nil
	})
}
func (s *Store) SavePlan(p access.Plan) (access.Plan, error) {
	if err := p.Validate(); err != nil {
		return p, err
	}
	err := s.consumerTransaction(func() error {
		if !s.groupExistsLocked(p.GroupID) {
			return fmt.Errorf("套餐分组不存在")
		}
		old := s.planLocked(p.ID)
		if p.ID != "" && old == nil {
			return fmt.Errorf("套餐不存在")
		}
		if old == nil {
			if len(s.st.Consumer.Plans) >= 128 {
				return fmt.Errorf("套餐数量已达上限")
			}
			p.ID = rand.Text()
			s.st.Consumer.Plans = append(s.st.Consumer.Plans, p)
		} else {
			if (old.GroupID != p.GroupID || old.Days != p.Days || old.MaxConcurrency != p.MaxConcurrency || old.Budget != p.Budget || old.Period != p.Period) && s.planReferencesLocked()[p.ID] != (PlanReferences{}) {
				return fmt.Errorf("已有订阅或兑换码引用，修改权益请复制为新套餐")
			}
			*old = p
		}
		return nil
	})
	return p, err
}
func periodStart(period string, now int64) int64 {
	t := time.Unix(now, 0).UTC()
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "day":
		return d.Unix()
	case "week":
		return d.AddDate(0, 0, -(int(d.Weekday())+6)%7).Unix()
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Unix()
	}
	return 0
}
func subscriptionUsage(sub access.Subscription, now int64) BudgetUsage {
	if periodStart(sub.Plan.Period, now) != sub.PeriodStart {
		return BudgetUsage{}
	}
	return sub.Usage
}
func (s *Store) assignSubscriptionLocked(u *access.User, p access.Plan, days int) error {
	if (!p.Enabled && !p.Delisted) || !s.groupExistsLocked(p.GroupID) {
		return fmt.Errorf("套餐已停用或分组不存在")
	}
	now := time.Now().Unix()
	if u.Subscription.ID != "" && u.Subscription.Plan.ID == p.ID && !u.Subscription.Disabled && u.Subscription.ExpiresAt > now {
		// Renew the same immutable entitlement; editing a template does not reset usage.
		u.Subscription.ExpiresAt += int64(days) * 86400
		p = u.Subscription.Plan
	} else {
		u.Subscription = access.Subscription{ID: rand.Text(), Plan: p, ExpiresAt: now + int64(days)*86400, PeriodStart: periodStart(p.Period, now)}
	}
	if u.Subscription.ExpiresAt > now+36500*86400 {
		return fmt.Errorf("订阅有效期不能超过 100 年")
	}
	u.GroupID = p.GroupID
	for _, k := range s.st.Keys {
		if k.UserID == u.ID {
			k.GroupID = p.GroupID
		}
	}
	return nil
}
func (s *Store) ManageSubscription(id, planID, action string, days int) error {
	return s.consumerTransaction(func() error {
		u, err := s.idleConsumerLocked(id)
		if err != nil {
			return err
		}
		switch action {
		case "assign":
			p := s.planLocked(planID)
			if p == nil {
				return fmt.Errorf("套餐不存在")
			}
			if !p.Enabled {
				return fmt.Errorf("套餐已下架或停用，不能分配或手动续期")
			}
			if days == 0 {
				days = p.Days
			}
			if days < 1 || days > 3650 {
				return fmt.Errorf("有效天数须为 1–3650")
			}
			return s.assignSubscriptionLocked(u, *p, days)
		case "disable":
			if u.Subscription.ID == "" {
				return fmt.Errorf("用户没有订阅")
			}
			u.Subscription.Disabled = true
		case "enable":
			if u.Subscription.ID == "" {
				return fmt.Errorf("用户没有订阅")
			}
			if !s.groupExistsLocked(u.Subscription.Plan.GroupID) {
				return access.ErrGroupUnavailable
			}
			u.Subscription.Disabled = false
		case "remove":
			u.Subscription = access.Subscription{} // explicit admin action returns to base group policy
		default:
			return fmt.Errorf("不支持的订阅操作")
		}
		return nil
	})
}
func (s *Store) consumerPermissionLocked(u *access.User, now int64) error {
	sub := u.Subscription
	if sub.ID != "" {
		if err := consumerStatus(u, now); err != nil {
			return err
		}
		if err := checkUserReservation(sub.Plan.Budget, subscriptionUsage(sub, now), s.budgetActive[userBudgetID(u.ID)]); err != nil {
			return err
		}
	}
	if u.Wallet.Enabled && u.Wallet.Balance <= 0 {
		return &BudgetError{Message: "额度不足，请联系管理员"}
	}
	return nil
}
func effectiveConcurrency(u *access.User) int {
	n := u.MaxConcurrency
	if u.Subscription.ID != "" && u.Subscription.Plan.MaxConcurrency > 0 && (n == 0 || u.Subscription.Plan.MaxConcurrency < n) {
		n = u.Subscription.Plan.MaxConcurrency
	}
	return n
}

// Generate returns plaintext once; durable state and list responses contain only
// a digest and suffix. Used codes are retained so they cannot be redeemed twice.
func (s *Store) GenerateRedemptions(template access.Redemption, count int) ([]string, error) {
	if count < 1 || count > 100 || template.ExpiresAt <= time.Now().Unix() {
		return nil, fmt.Errorf("数量须为 1–100，且有效期必须在未来")
	}
	codes := []string{}
	err := s.consumerTransaction(func() error {
		if len(s.st.Consumer.Redemptions)+count > 10000 {
			return fmt.Errorf("兑换码数量已达上限")
		}
		switch template.Kind {
		case "balance":
			if template.Amount <= 0 || template.Amount > access.MaxCredits {
				return fmt.Errorf("兑换额度无效")
			}
		case "concurrency":
			if template.Concurrency < 1 || access.ValidateUserConcurrency(template.Concurrency) != nil {
				return fmt.Errorf("并发增量无效")
			}
		case "subscription":
			p := s.planLocked(template.PlanID)
			if p == nil || !p.Enabled {
				return fmt.Errorf("套餐不存在或已停用")
			}
		default:
			return fmt.Errorf("兑换类型无效")
		}
		for range count {
			code := "redeem-" + rand.Text() + rand.Text()
			r := template
			r.ID = rand.Text()
			r.Digest = access.CodeDigest(code)
			r.Hint = "…" + code[len(code)-6:]
			r.UsedBy = ""
			r.UsedAt = 0
			r.Disabled = false
			s.st.Consumer.Redemptions = append(s.st.Consumer.Redemptions, r)
			codes = append(codes, code)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}
func (s *Store) Redemptions() []access.Redemption {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]access.Redemption{}, s.st.Consumer.Redemptions...)
	for i := range out {
		out[i].Digest = ""
	}
	return out
}
func (s *Store) DisableRedemption(id string) error {
	return s.consumerTransaction(func() error {
		for i := range s.st.Consumer.Redemptions {
			r := &s.st.Consumer.Redemptions[i]
			if r.ID == id {
				r.Disabled = true
				return nil
			}
		}
		return fmt.Errorf("兑换码不存在")
	})
}
func (s *Store) Redeem(id, code string) error {
	if len(code) > 200 || len(code) < 20 {
		return fmt.Errorf("兑换码无效、已使用或已过期")
	}
	digest := access.CodeDigest(code)
	return s.consumerTransaction(func() error {
		u, err := s.idleConsumerLocked(id)
		if err != nil {
			return err
		}
		if !u.Enabled {
			return access.ErrUserAuthentication
		}
		for i := range s.st.Consumer.Redemptions {
			r := &s.st.Consumer.Redemptions[i]
			if r.Digest != digest {
				continue
			}
			if r.Disabled || r.UsedBy != "" || r.ExpiresAt <= time.Now().Unix() {
				break
			}
			switch r.Kind {
			case "balance":
				if u.Wallet.Balance+r.Amount > access.MaxCredits {
					return fmt.Errorf("兑换后额度超出范围")
				}
				u.Wallet.Balance += r.Amount
				u.Wallet.Revision++
				u.Wallet.Enabled = true
				s.ledgerLocked(u, r.Amount, "redeem", r.ID, "兑换额度")
			case "concurrency":
				if u.MaxConcurrency == 0 && (u.Subscription.ID == "" || u.Subscription.Plan.MaxConcurrency == 0) {
					return fmt.Errorf("当前并发不限，无需兑换并发增量")
				}
				if u.MaxConcurrency > 0 {
					if err := access.ValidateUserConcurrency(u.MaxConcurrency + r.Concurrency); err != nil {
						return err
					}
					u.MaxConcurrency += r.Concurrency
				}
				if u.Subscription.ID != "" && u.Subscription.Plan.MaxConcurrency > 0 {
					if err := access.ValidateUserConcurrency(u.Subscription.Plan.MaxConcurrency + r.Concurrency); err != nil {
						return err
					}
					u.Subscription.Plan.MaxConcurrency += r.Concurrency
				}
			case "subscription":
				p := s.planLocked(r.PlanID)
				if p == nil {
					return fmt.Errorf("套餐不存在")
				}
				if err := s.assignSubscriptionLocked(u, *p, p.Days); err != nil {
					return err
				}
			default:
				return fmt.Errorf("兑换类型无效")
			}
			r.UsedBy = id
			r.UsedAt = time.Now().Unix()
			return nil
		}
		return fmt.Errorf("兑换码无效、已使用或已过期")
	})
}

func consumerStatus(u *access.User, now int64) error {
	sub := u.Subscription
	if sub.ID != "" && (sub.Disabled || now >= sub.ExpiresAt || u.GroupID != sub.Plan.GroupID) {
		return &BudgetError{Message: "用户订阅已停用、到期或分组不匹配"}
	}
	return nil
}

func (s *Store) UserSubscription(id string) access.Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u := s.userLocked(id); u != nil {
		sub := u.Subscription
		sub.Usage = subscriptionUsage(sub, time.Now().Unix())
		sub.PeriodStart = periodStart(sub.Plan.Period, time.Now().Unix())
		return sub
	}
	return access.Subscription{}
}
func (s *Store) UserReservedCredits(id string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.consumerReserved[id]
}

func (s *Store) UserRedemptions(id string) []access.Redemption {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []access.Redemption{}
	for _, r := range s.st.Consumer.Redemptions {
		if r.UsedBy == id {
			r.Digest = ""
			out = append(out, r)
		}
	}
	return out
}
