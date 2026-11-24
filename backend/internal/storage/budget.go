package storage

import (
	"codearts2api/internal/usage"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
)

type KeyBudget = access.KeyBudget
type BudgetUsage = access.BudgetUsage
type BudgetError = access.BudgetError

func budgetCheck(b KeyBudget, u BudgetUsage) error { return access.CheckBudget(b, u) }

func budgetOwnerError(err error) error {
	if err == access.ErrGroupUnavailable {
		return config.ModelFailure(403, "group_unavailable", err.Error())
	}
	return &BudgetError{Message: err.Error()}
}

// Count in-flight requests toward request-count limits until settlement.
func (s *Store) BeginKeyBudget(key string) (func(), error) { return s.beginKeyBudget(key, nil) }
func (s *Store) BeginRequestBudget(key string) (func(), bool, error) {
	var metered bool
	release, err := s.beginKeyBudget(key, &metered)
	return release, metered, err
}
func (s *Store) beginKeyBudget(key string, metered *bool) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.journalErr != nil {
		return nil, ErrUsagePersistence
	}
	return s.reserveBudgetLocked(key, metered, false)
}
func (s *Store) reserveBudgetLocked(key string, metered *bool, search bool) (func(), error) {
	for _, k := range s.st.Keys {
		if k.Key == key {
			if !k.Enabled {
				return nil, &BudgetError{Message: "密钥已停用"}
			}
			if err := s.keyOwnerLocked(k); err != nil {
				return nil, budgetOwnerError(err)
			}
			if err := budgetCheck(k.Budget, k.BudgetUsage); !search && err != nil {
				return nil, err
			}
			active := s.budgetActive[key]
			if k.Budget.MaxRequests > 0 && k.BudgetUsage.Requests+int64(active) >= k.Budget.MaxRequests {
				return nil, &BudgetError{Message: "请求次数预算已被在途请求预留"}
			}

			if s.budgetActive == nil {
				s.budgetActive = map[string]int{}
			}
			owner := ""
			if k.UserID != "" {
				u := s.userLocked(k.UserID)
				if metered != nil {
					*metered = u.Wallet.Enabled || u.Subscription.ID != ""
				}
				owner = userBudgetID(u.ID)
				if search {
					sub := u.Subscription
					if sub.ID != "" {
						if err := checkUserReservation(KeyBudget{MaxRequests: sub.Plan.Budget.MaxRequests}, subscriptionUsage(sub, time.Now().Unix()), s.budgetActive[owner]); err != nil {
							return nil, err
						}
					}
				} else if err := s.consumerPermissionLocked(u, time.Now().Unix()); err != nil {
					return nil, err
				}
				if effectiveConcurrency(u) > 0 && s.budgetActive[owner] >= effectiveConcurrency(u) {
					return nil, &access.UserConcurrencyError{}
				}
				budget := u.Budget
				if search {
					budget = KeyBudget{MaxRequests: budget.MaxRequests}
				}
				if err := checkUserReservation(budget, u.BudgetUsage, s.budgetActive[owner]); err != nil {
					return nil, err
				}
				s.budgetActive[owner]++
			}
			s.budgetActive[key]++
			var once sync.Once
			return func() {
				once.Do(func() {
					s.mu.Lock()
					defer s.mu.Unlock()
					s.budgetActive[key]--
					if s.budgetActive[key] <= 0 {
						delete(s.budgetActive, key)
					}
					if owner != "" {
						s.budgetActive[owner]--
						if s.budgetActive[owner] <= 0 {
							delete(s.budgetActive, owner)
						}
					}
				})
			}, nil
		}
	}
	return func() {}, nil // Local anonymous access has no key-specific budget.
}

func (s *Store) CheckKeyBudgetProgress(key, model string, attempts []RequestAttempt) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkProgressLocked(key, s.priceLocked(model), attempts)
}
func (s *Store) checkProgressLocked(key string, price usage.AttemptPrice, attempts []RequestAttempt) error {
	if s.journalErr != nil {
		return ErrUsagePersistence
	}
	for _, k := range s.st.Keys {
		if k.Key == key {
			if !k.Enabled {
				return &BudgetError{Message: "密钥已停用"}
			}
			if err := s.keyOwnerLocked(k); err != nil {
				return budgetOwnerError(err)
			}
			usage := k.BudgetUsage
			for _, a := range attempts {
				usage.Tokens += a.Usage.InputTokens + a.Usage.OutputTokens
				if c := s.attemptCostLocked(a); c != nil {
					usage.USD += c.Amount
				}
			}
			if k.Budget.MaxUSD > 0 && price.Price == nil {
				return &BudgetError{Message: "该模型未配置 USD 价格，无法执行费用预算；请先设置价格"}
			}
			// The current request already owns its request-count reservation.
			b := k.Budget
			b.MaxRequests = 0
			if err := budgetCheck(b, usage); err != nil {
				return err
			}
			if k.UserID != "" {
				u := s.userLocked(k.UserID)
				b, total := u.Budget, u.BudgetUsage
				for _, a := range attempts {
					total.Tokens += a.Usage.InputTokens + a.Usage.OutputTokens
					if c := s.attemptCostLocked(a); c != nil {
						total.USD += c.Amount
					}
				}
				if b.MaxUSD > 0 && price.Price == nil {
					return &BudgetError{Message: "该模型缺少价格，无法执行用户费用预算"}
				}
				b.MaxRequests = 0
				return checkUserBudget(b, total)
			}
			return nil
		}
	}
	return nil
}

func (s *Store) SetKeyBudgetAndSave(key string, b KeyBudget, reset bool) error {
	if b.MaxRequests < 0 || b.MaxTokens < 0 || b.MaxUSD < 0 || math.IsNaN(b.MaxUSD) || math.IsInf(b.MaxUSD, 0) {
		return fmt.Errorf("预算须为非负数，0 表示不限")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.budgetActive[key] > 0 {
		return fmt.Errorf("该密钥仍有在途请求，请完成后再修改预算")
	}
	for _, k := range s.st.Keys {
		if k.Key == key {
			before := *k
			k.Budget = b
			if reset {
				k.BudgetUsage = BudgetUsage{}
			}
			raw, err := json.MarshalIndent(s.st, "", "  ")
			if err == nil {
				err = s.writeState(raw)
			}
			if err != nil {
				*k = before
			}
			return err
		}
	}
	return fmt.Errorf("密钥不存在")
}

// Search supplies no measured token or cost receipt. Do not silently bypass a
// metered budget or poison it with an unmeasurable operation after admission.
func (s *Store) CheckSearchBudget(key string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.st.Keys {
		if k.Key == key {
			if err := s.keyOwnerLocked(k); err != nil {
				return budgetOwnerError(err)
			}
			metered := k.Budget.MaxTokens > 0 || k.Budget.MaxUSD > 0
			if k.UserID != "" {
				u := s.userLocked(k.UserID)
				metered = metered || u.Budget.MaxTokens > 0 || u.Budget.MaxUSD > 0 || u.Wallet.Enabled || u.Subscription.Plan.Budget.MaxTokens > 0 || u.Subscription.Plan.Budget.MaxUSD > 0
			}
			if metered {
				return &BudgetError{Message: "搜索未提供 Token 或费用回执，用户及密钥只能设置请求次数预算"}
			}
		}
	}
	return nil
}

func checkUserBudget(b KeyBudget, u BudgetUsage) error {
	if err := budgetCheck(b, u); err != nil {
		return &BudgetError{Message: "用户共享预算：" + err.Error()}
	}
	return nil
}
func checkUserReservation(b KeyBudget, u BudgetUsage, active int) error {
	if err := checkUserBudget(b, u); err != nil {
		return err
	}
	if b.MaxRequests > 0 && u.Requests+int64(active) >= b.MaxRequests {
		return &BudgetError{Message: "用户请求预算已被在途请求预留"}
	}

	return nil
}
func addBudgetUsage(u *BudgetUsage, e LogEntry) {
	u.Requests++
	if e.SearchServiceID != "" {
		return
	}
	u.Tokens += e.Usage.InputTokens + e.Usage.OutputTokens
	if e.EstimatedCost != nil {
		u.USD += e.EstimatedCost.Amount
	}
	if e.Usage.Known && (e.EstimatedCost == nil || e.EstimatedCost.Partial) {
		u.Unpriced = true
	}
	if e.UsageIncomplete || !e.Usage.Known {
		u.Incomplete = true
	}
}
