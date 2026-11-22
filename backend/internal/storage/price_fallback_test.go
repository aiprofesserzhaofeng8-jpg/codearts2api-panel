package storage

import (
	"codearts2api/internal/usage"
	"math"
	"testing"
)

func TestAttemptPricePriorityRestartAndRetries(t *testing.T) {
	s := testPool(t, 0)
	key := groupedTestKey(t, s, "price fixture").Key
	if err := s.SetKeyBudgetAndSave(key, KeyBudget{MaxUSD: 1}, false); err != nil {
		t.Fatal(err)
	}
	p := ModelPrice{Currency: "USD", Input: 2, Output: 6, Cached: 1}
	if err := s.SetDefaultPriceAndSave(&p); err != nil {
		t.Fatal(err)
	}
	s, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	a := RequestAttempt{Model: "trae/cn/kimi-k3", Usage: TokenUsage{Known: true, InputTokens: 1000, OutputTokens: 100, CachedTokens: 200}}
	b := a
	b.Model = "opencode/go/kimi-k3"
	a.Quote, err = s.AdmitAttempt(key, "", a.Model, 0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.Quote, err = s.AdmitAttempt(key, "", b.Model, 0, false, []RequestAttempt{a})
	if err != nil {
		t.Fatal(err)
	}
	if a.Quote.Kind != "default" || b.Quote.Kind != "reference" || b.Quote.Price.Input != 3 {
		t.Fatal(a.Quote, b.Quote)
	}
	if err = s.SetPriceAndSave(b.Model, ModelPrice{Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.AdmitAttempt(key, "", b.Model, 0, false, nil)
	if err != nil || c.Kind != "model" || c.Estimate(b.Usage).Amount != 0 {
		t.Fatal(c, err)
	}
	if err = s.SetDefaultPriceAndSave(nil); err != nil {
		t.Fatal(err)
	}
	// Now the second source is explicitly free, and the first has no current price.
	entry := LogEntry{Model: b.Model, Usage: a.Usage, Attempts: 2, AttemptDetails: []RequestAttempt{a, b}}
	if err = s.Record(entry, 0, key); err != nil {
		t.Fatal(err)
	}
	log := s.Logs(1)[0]
	if cost := log.EstimatedCost; cost == nil || cost.PriceKind != "mixed" || math.Abs(cost.Amount-(.0024+.00396)) > 1e-12 || log.AttemptDetails[0].EstimatedCost.PriceKind != "default" || log.AttemptDetails[1].EstimatedCost.PriceKind != "reference" {
		t.Fatal(cost, log.AttemptDetails)
	}
	if _, err = s.AdmitAttempt(key, "", a.Model, 0, false, nil); err == nil {
		t.Fatal("cleared default accepted")
	}
	if s.DefaultTokenPrice() != nil || c.Estimate(usage.TokenUsage{}) != nil {
		t.Fatal("missing usage or default treated as zero")
	}
}

func TestDefaultPriceFailedWriteKeepsActivePrice(t *testing.T) {
	s := testPool(t, 0)
	original := ModelPrice{Currency: "USD", Input: 2}
	if err := s.SetDefaultPriceAndSave(&original); err != nil {
		t.Fatal(err)
	}
	path := s.path
	// A directory cannot be replaced by the atomic state file rename.
	s.path = t.TempDir()
	if err := s.SetDefaultPriceAndSave(nil); err == nil {
		t.Fatal("expected persistence failure")
	}
	s.path = path
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []*Store{s, reopened} {
		if got := st.DefaultTokenPrice(); got == nil || got.Input != 2 {
			t.Fatal(got)
		}
	}
}
