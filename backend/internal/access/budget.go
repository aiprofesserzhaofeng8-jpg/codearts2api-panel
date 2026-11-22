package access

import (
	"fmt"
	"math"
)

func ValidateBudget(b KeyBudget) error {
	if b.MaxRequests < 0 || b.MaxTokens < 0 || b.MaxUSD < 0 || math.IsNaN(b.MaxUSD) || math.IsInf(b.MaxUSD, 0) {
		return fmt.Errorf("预算须为非负数，0 表示不限")
	}
	return nil
}

type BudgetError struct{ Message string }

func (e *BudgetError) Error() string             { return e.Message }
func (e *BudgetError) BudgetExceeded() bool      { return true }
func (e *BudgetError) NonRetryableRequest() bool { return true }
func CheckBudget(b KeyBudget, u BudgetUsage) error {
	if b.MaxRequests > 0 && u.Requests >= b.MaxRequests {
		return &BudgetError{Message: "该密钥的请求次数预算已用完"}
	}
	if b.MaxTokens > 0 && u.Tokens >= b.MaxTokens {
		return &BudgetError{Message: "该密钥的 Token 预算已用完"}
	}
	if b.MaxUSD > 0 && u.USD >= b.MaxUSD {
		return &BudgetError{Message: "该密钥的 USD 参考费用预算已用完"}
	}
	return nil
}
