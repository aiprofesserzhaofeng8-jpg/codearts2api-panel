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
	return s.reserveQuoteLocked(key, id, s.priceLocked(model), inputBytes, attempts)
}

// AdmitAttempt checks limits and reserves wallet funds at the very same rate.
func (s *Store) AdmitAttempt(key, id, model string, inputBytes int, metered bool, attempts []RequestAttempt) (*usage.AttemptPrice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	price := s.priceLocked(model)
	if err := s.checkProgressLocked(key, price, attempts); err != nil {
		return nil, err
	}
	if metered {
		if err := s.reserveQuoteLocked(key, id, price, inputBytes, attempts); err != nil {
			return nil, err
		}
	}
	return &price, nil
}
func (s *Store) reserveQuoteLocked(key, id string, price usage.AttemptPrice, inputBytes int, attempts []RequestAttempt) error {
	var u *access.User
	for _, k := range s.st.Keys {
		if k.Key == key {
			if err := s.keyOwnerLocked(k); err != nil {
				return budgetOwnerError(err)
			}
			if k.UserID == "" {
				return nil
			}
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
			if c := s.attemptCostLocked(a); c != nil {
				total.USD += c.Amount
			}
		}
		if b.MaxUSD > 0 && price.Price == nil {
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
	quote := price.Estimate(TokenUsage{Known: true, InputTokens: int64(max(0, inputBytes)), OutputTokens: 8192})
	if quote == nil {
		return &BudgetError{Message: "模型未配置参考价格，无法预留额度"}
	}
	cost := quote.Amount
	for _, a := range attempts {
		c := s.attemptCostLocked(a)
		if c == nil {
			continue
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
	if e.SearchServiceID != "" || e.UserID == "" || e.Attempts == 0 {
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
