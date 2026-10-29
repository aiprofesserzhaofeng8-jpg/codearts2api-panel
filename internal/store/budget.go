package store

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

type KeyBudget struct {
	MaxRequests int64   `json:"max_requests"`
	MaxTokens   int64   `json:"max_tokens"`
	MaxUSD      float64 `json:"max_usd"`
}
type BudgetUsage struct {
	Unpriced   bool    `json:"unpriced"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	USD        float64 `json:"usd"`
	Incomplete bool    `json:"incomplete"`
}
type BudgetError struct{ Message string }

func (e *BudgetError) Error() string             { return e.Message }
func (e *BudgetError) BudgetExceeded() bool      { return true }
func (e *BudgetError) NonRetryableRequest() bool { return true }
func budgetCheck(b KeyBudget, u BudgetUsage) error {
	if b.MaxUSD > 0 && u.Unpriced {
		return &BudgetError{"已有请求缺少模型价格，USD 预算无法核算；请检查后重置预算"}
	}
	if (b.MaxTokens > 0 || b.MaxUSD > 0) && u.Incomplete {
		return &BudgetError{"上游用量缺失，预算无法核算；请在密钥预算中检查并重置"}
	}
	if b.MaxRequests > 0 && u.Requests >= b.MaxRequests {
		return &BudgetError{"该密钥的请求次数预算已用完"}
	}
	if b.MaxTokens > 0 && u.Tokens >= b.MaxTokens {
		return &BudgetError{"该密钥的 Token 预算已用完"}
	}
	if b.MaxUSD > 0 && u.USD >= b.MaxUSD {
		return &BudgetError{"该密钥的 USD 参考费用预算已用完"}
	}
	return nil
}

// One active inference for keys with metered limits bounds soft-budget overrun
// to one request. Request reservations also prevent concurrent count overruns.
func (s *Store) BeginKeyBudget(key string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.st.Keys {
		if k.Key == key {
			if !k.Enabled {
				return nil, &BudgetError{"密钥已停用"}
			}
			if err := budgetCheck(k.Budget, k.BudgetUsage); err != nil {
				return nil, err
			}
			active := s.budgetActive[key]
			if k.Budget.MaxRequests > 0 && k.BudgetUsage.Requests+int64(active) >= k.Budget.MaxRequests {
				return nil, &BudgetError{"请求次数预算已被在途请求预留"}
			}
			if active > 0 && (k.Budget.MaxTokens > 0 || k.Budget.MaxUSD > 0) {
				return nil, &BudgetError{"该密钥正在结算一个请求，请稍后再试"}
			}
			if s.budgetActive == nil {
				s.budgetActive = map[string]int{}
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
				})
			}, nil
		}
	}
	return func() {}, nil // Local anonymous access has no key-specific budget.
}

func (s *Store) CheckKeyBudgetProgress(key, model string, attempts []RequestAttempt) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.st.Keys {
		if k.Key == key {
			if !k.Enabled {
				return &BudgetError{"密钥已停用"}
			}
			usage := k.BudgetUsage
			for _, a := range attempts {
				usage.Tokens += a.Usage.InputTokens + a.Usage.OutputTokens
				if !a.Usage.Known {
					usage.Incomplete = true
				}
				if c := s.estimateModelLocked(a.Model, a.Usage); c != nil {
					usage.USD += c.Amount
				}
			}
			if k.Budget.MaxUSD > 0 && s.estimateModelLocked(model, TokenUsage{Known: true}) == nil {
				return &BudgetError{"该模型未配置 USD 价格，无法执行费用预算；请先设置价格"}
			}
			// The current request already owns its request-count reservation.
			b := k.Budget
			b.MaxRequests = 0
			return budgetCheck(b, usage)
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
		if k.Key == key && (k.Budget.MaxTokens > 0 || k.Budget.MaxUSD > 0) {
			return &BudgetError{"搜索未提供 Token 或费用回执，请使用仅设置请求次数预算的密钥"}
		}
	}
	return nil
}
