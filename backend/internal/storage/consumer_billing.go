package storage

import (
	"codearts2api/internal/access"
	"codearts2api/internal/usage"
	"fmt"
	"time"
)

type consumerHold struct {
	UserID string
	Amount int64
}

// ReserveConsumerQuote is called once per actual attempt, after model mapping.
// Updating the same request replaces its hold, preserving a single retry budget.
// Unknown usage fails closed for metered consumers; unlimited users skip pricing.
func (s *Store) ReserveConsumerQuote(key, id, model string, inputBytes int, attempts []RequestAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var u *access.User
	for _, k := range s.st.Keys {
		if k.Key == key && k.UserID != "" {
			u = s.userLocked(k.UserID)
			break
		}
	}
	if u == nil {
		return nil
	}
	if u.Subscription.ID != "" {
		sub := u.Subscription
		b, total := sub.Plan.Budget, subscriptionUsage(sub, time.Now().Unix())
		b.MaxRequests = 0
		for _, a := range attempts {
			total.Tokens += a.Usage.InputTokens + a.Usage.OutputTokens
			if !a.Usage.Known {
				total.Incomplete = true
			}
			if c := s.estimateModelLocked(a.Model, a.Usage); c != nil {
				total.USD += c.Amount
			} else if a.Usage.Known {
				total.Unpriced = true
			}
		}
		if b.MaxUSD > 0 && s.estimateModelLocked(model, TokenUsage{Known: true}) == nil {
			return &BudgetError{Message: "套餐费用预算要求模型具有价格"}
		}
		if err := checkUserBudget(b, total); err != nil {
			return err
		}
	}
	if !u.Wallet.Enabled {
		return nil
	}
	if id == "" {
		return &BudgetError{Message: "缺少请求结算标识，无法预留额度"}
	}
	if u.Wallet.ReviewRequired {
		return &BudgetError{Message: "存在用量待核对，暂停额度调用"}
	}
	quote := s.estimateModelLocked(model, TokenUsage{Known: true, InputTokens: int64(max(0, inputBytes)), OutputTokens: 8192})
	if quote == nil {
		return &BudgetError{Message: "模型未配置参考价格，无法预留额度"}
	}
	cost := quote.Amount
	for _, a := range attempts {
		c := s.estimateModelLocked(a.Model, a.Usage)
		if !a.Usage.Known || c == nil {
			return &BudgetError{Message: "前次尝试缺少用量或价格，已停止自动重试"}
		}
		cost += c.Amount
	}
	amount, err := access.CostCredits(cost)
	if err != nil {
		return &BudgetError{Message: err.Error()}
	}
	old := s.consumerHolds[id]
	if old.UserID != "" && old.UserID != u.ID {
		return fmt.Errorf("请求预留归属冲突")
	}
	available := u.Wallet.Balance - s.consumerReserved[u.ID] + old.Amount
	if available < amount {
		return &BudgetError{Message: "可用额度不足以预留本次请求，请补充额度"}
	}
	if s.consumerHolds == nil {
		s.consumerHolds = map[string]consumerHold{}
		s.consumerReserved = map[string]int64{}
	}
	s.consumerHolds[id] = consumerHold{u.ID, amount}
	s.consumerReserved[u.ID] += amount - old.Amount
	return nil
}
func (s *Store) releaseConsumerQuoteLocked(id string) {
	if h, ok := s.consumerHolds[id]; ok {
		s.consumerReserved[h.UserID] -= h.Amount
		if s.consumerReserved[h.UserID] <= 0 {
			delete(s.consumerReserved, h.UserID)
		}
		delete(s.consumerHolds, id)
	}
}
func (s *Store) ReleaseConsumerQuote(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	s.releaseConsumerQuoteLocked(id)
	s.mu.Unlock()
}
func (s *Store) prepareConsumerSettlementLocked(e *LogEntry) {
	if e.UserID == "" || e.Attempts == 0 {
		return
	}
	u := s.userLocked(e.UserID)
	if u == nil || !u.Wallet.Enabled {
		return
	}
	charge := &usage.ConsumerCharge{Review: !e.Usage.Known || e.UsageIncomplete || e.EstimatedCost == nil}
	if e.EstimatedCost != nil {
		charge.Review = charge.Review || e.EstimatedCost.Partial
		amount, err := access.CostCredits(e.EstimatedCost.Amount)
		if err != nil {
			charge.Review = true
		} else {
			charge.Units = amount
		}
	}
	e.ConsumerCharge = charge
}
func (s *Store) applyConsumerSettlementLocked(u *access.User, e LogEntry) {
	if e.ConsumerCharge != nil {
		u.Wallet.Balance -= e.ConsumerCharge.Units
		u.Wallet.Revision++
		u.Wallet.ReviewRequired = u.Wallet.ReviewRequired || e.ConsumerCharge.Review
		s.ledgerLocked(u, -e.ConsumerCharge.Units, "usage", e.RequestID, "模型实际用量结算")
		last := &s.st.Consumer.Ledger[len(s.st.Consumer.Ledger)-1]
		last.ID = fmt.Sprintf("usage:%d", s.st.UsageSequence+1)
		last.Time = e.Time
	}
	if u.Subscription.ID != "" {
		sub := &u.Subscription
		start := periodStart(sub.Plan.Period, e.Time)
		if start != sub.PeriodStart {
			sub.PeriodStart = start
			sub.Usage = BudgetUsage{}
		}
		addBudgetUsage(&sub.Usage, e)
	}
	s.releaseConsumerQuoteLocked(e.RequestID)
}
