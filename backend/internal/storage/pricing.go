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
	return s.priceLocked(model).Estimate(u)
}
func (s *Store) priceLocked(model string) usage.AttemptPrice {
	kind := "model"
	p, ok := s.st.Prices[model]
	if !ok {
		kind = "reference"
		p, ok = usage.ReferencePrice(model)
	}
	if !ok {
		if s.st.DefaultTokenPrice == nil {
			return usage.AttemptPrice{Model: model}
		}
		kind, p = "default", *s.st.DefaultTokenPrice
	}
	return usage.AttemptPrice{Model: model, Kind: kind, Price: &p}
}
func (s *Store) attemptCostLocked(a RequestAttempt) *RequestCost {
	if a.Quote != nil {
		return a.Quote.Estimate(a.Usage)
	}
	return s.estimateModelLocked(a.Model, a.Usage)
}
func (s *Store) costForEntryLocked(e *LogEntry) *RequestCost {
	if len(e.AttemptDetails) == 0 {
		return s.estimateModelLocked(e.Model, e.Usage)
	}
	total := &RequestCost{Currency: "USD"}
	known := false
	for i := range e.AttemptDetails {
		a := &e.AttemptDetails[i]
		a.EstimatedCost = s.attemptCostLocked(*a)
		a.Quote = nil
		if a.EstimatedCost == nil {
			total.Partial = true
			continue
		}
		if !known {
			total.Price = a.EstimatedCost.Price
		}
		known = true
		total.Amount += a.EstimatedCost.Amount
		total.Partial = total.Partial || a.EstimatedCost.Partial
		if total.PriceKind == "" {
			total.PriceKind = a.EstimatedCost.PriceKind
		} else if total.PriceKind != a.EstimatedCost.PriceKind {
			total.PriceKind = "mixed"
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

func (s *Store) DefaultTokenPrice() *ModelPrice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.st.DefaultTokenPrice == nil {
		return nil
	}
	p := *s.st.DefaultTokenPrice
	return &p
}
func (s *Store) SetDefaultPriceAndSave(price *ModelPrice) error {
	if price != nil {
		p := *price
		if err := p.Validate(); err != nil {
			return err
		}
		p.Source = "全站默认价"
		p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		price = &p
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.st.DefaultTokenPrice
	s.st.DefaultTokenPrice = price
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err == nil {
		err = s.writeState(raw)
	}
	if err != nil {
		s.st.DefaultTokenPrice = old
	}
	return err
}
