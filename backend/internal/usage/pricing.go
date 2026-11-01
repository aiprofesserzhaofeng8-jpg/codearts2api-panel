// Package usage owns price values and management rules, not ledger transactions.
package usage

import (
	"fmt"
	"math"
)

// Prices are currency units per million tokens, not Huawei credit rates.
type ModelPrice struct {
	Currency  string  `json:"currency"`
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Cached    float64 `json:"cached"`
	Source    string  `json:"source"`
	UpdatedAt string  `json:"updated_at"`
}

func (p ModelPrice) Validate() error {
	if p.Currency != "USD" {
		return fmt.Errorf("币种必须为 USD")
	}
	for _, n := range []float64{p.Input, p.Output, p.Cached} {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e6 {
			return fmt.Errorf("每百万 Token 单价必须在 0 至 1000000 之间")
		}
	}
	if len(p.Source) > 1000 {
		return fmt.Errorf("价格来源过长")
	}
	return nil
}
