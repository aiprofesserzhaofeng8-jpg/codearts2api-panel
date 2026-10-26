package store

import (
	"math"
	"path/filepath"
	"testing"
)

func TestPricingSnapshotPersistenceAndCurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := ModelPrice{Currency: "USD", Input: 2, Output: 8, Cached: .04}
	if err := s.SetPriceAndSave("test", p); err != nil {
		t.Fatal(err)
	}
	u := TokenUsage{Known: true, InputTokens: 1000000, OutputTokens: 100000, CachedTokens: 500000}
	s.Record(LogEntry{Status: 200, Model: "test", Usage: u}, 0)
	cost := s.Logs(1)[0].EstimatedCost
	if cost == nil || math.Abs(cost.Amount-1.82) > 1e-10 {
		t.Fatalf("cache counted incorrectly: %+v", cost)
	}
	p.Currency = "USD"
	p.Input = 4
	if err := s.SetPriceAndSave("test", p); err != nil {
		t.Fatal(err)
	}
	s.Record(LogEntry{Status: 200, Model: "test", Usage: u}, 0)
	s.Record(LogEntry{Status: 200, Model: "unknown", Usage: u}, 0)
	s.Record(LogEntry{Status: 500, Model: "test", Usage: u}, 0)
	s.Record(LogEntry{Status: 200, Model: "test"}, 0)
	stats := s.Stats()
	if stats.PricedRequests != 3 || stats.PricedCNYRequests != 0 || stats.PricedUSDRequests != 3 || stats.EstimatedCNY != 0 || math.Abs(stats.EstimatedUSD-7.46) > 1e-10 {
		t.Fatalf("bad totals: %+v", stats)
	}
	if s.Logs(0)[0].EstimatedCost.Price.Input != 2 {
		t.Fatal("repriced history")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Stats() != stats || loaded.Prices()["test"].Input != 4 {
		t.Fatal("pricing not persisted")
	}
	loaded.ClearStats()
	if loaded.Stats().PricedRequests != 0 || loaded.Stats().EstimatedCNY != 0 {
		t.Fatal("reset failed")
	}
}
func TestInvalidAndZeroPrices(t *testing.T) {
	for _, p := range []ModelPrice{{Currency: "CNY"}, {Currency: "EUR"}, {Currency: "USD", Input: -1}, {Currency: "USD", Output: math.Inf(1)}} {
		if p.Validate() == nil {
			t.Fatal("invalid price accepted")
		}
	}
	p := ModelPrice{Currency: "USD"}
	if p.Validate() != nil {
		t.Fatal("explicit free price rejected")
	}
	if c := estimateCost(TokenUsage{Known: true, InputTokens: 100}, p); c == nil || c.Amount != 0 {
		t.Fatal("free not distinguished from unknown")
	}
}
