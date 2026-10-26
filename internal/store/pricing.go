package store

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
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
type RequestCost struct {
	Partial  bool       `json:"partial,omitempty"`
	Currency string     `json:"currency"`
	Amount   float64    `json:"amount"`
	Price    ModelPrice `json:"price"`
}

func (s *Store) estimateModelLocked(model string, u TokenUsage) *RequestCost {
	p, ok := s.st.Prices[model]
	if !ok {
		p, ok = defaultPrices()[model]
	}
	if !ok {
		return nil
	}
	return estimateCost(u, p)
}
func (s *Store) costForEntryLocked(e *LogEntry) *RequestCost {
	if len(e.AttemptDetails) == 0 {
		return s.estimateModelLocked(e.Model, e.Usage)
	}
	total := &RequestCost{Currency: "USD"}
	known := false
	for i := range e.AttemptDetails {
		a := &e.AttemptDetails[i]
		a.EstimatedCost = s.estimateModelLocked(a.Model, a.Usage)
		if a.EstimatedCost == nil {
			total.Partial = true
			continue
		}
		known = true
		total.Amount += a.EstimatedCost.Amount
		if i == 0 {
			total.Price = a.EstimatedCost.Price
		}
	}
	if !known {
		return nil
	}
	return total
}

func defaultPrices() map[string]ModelPrice {
	return map[string]ModelPrice{"deepseek-v4.1-flash": {Currency: "USD", Input: 0.30, Output: 1.20, Cached: 0.006,
		Source: "DeepSeek 官方高峰参考价（固定，不自动切换时段） https://api-docs.deepseek.com/quick_start/pricing/", UpdatedAt: "2026-10-26"}}
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
func estimateCost(u TokenUsage, p ModelPrice) *RequestCost {
	if !u.Known || p.Validate() != nil {
		return nil
	}
	cached := min(max(0, u.CachedTokens), max(0, u.InputTokens))
	amount := (float64(max(0, u.InputTokens)-cached)*p.Input + float64(cached)*p.Cached + float64(max(0, u.OutputTokens))*p.Output) / 1e6
	return &RequestCost{Currency: p.Currency, Amount: amount, Price: p}
}
func (s *Store) Prices() map[string]ModelPrice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := defaultPrices()
	for k, v := range s.st.Prices {
		if v.Currency == "USD" {
			out[k] = v
		}
	}
	return out
}
func (s *Store) SetPriceAndSave(model string, price ModelPrice) error {
	model = strings.TrimSpace(model)
	if model == "" || len(model) > 200 {
		return fmt.Errorf("模型 ID 无效")
	}
	if err := price.Validate(); err != nil {
		return err
	}
	price.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if price.Source == "" {
		price.Source = "管理员自定义参考价"
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.st.Prices
	next := make(map[string]ModelPrice, len(before)+1)
	for k, v := range before {
		next[k] = v
	}
	next[model] = price
	s.st.Prices = next
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err == nil {
		err = s.writeState(raw)
	}
	if err != nil {
		s.st.Prices = before
	}
	return err
}
