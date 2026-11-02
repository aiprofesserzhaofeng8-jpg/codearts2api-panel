package storage

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"codearts2api/internal/usage"
)

// ModelPrice preserves the existing state and public Go compatibility type.
type ModelPrice = usage.ModelPrice
type RequestCost = usage.RequestCost

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

func defaultPrices() map[string]ModelPrice                 { return usage.DefaultPrices() }
func estimateCost(u TokenUsage, p ModelPrice) *RequestCost { return usage.EstimateCost(u, p) }
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
