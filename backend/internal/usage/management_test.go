package usage

import (
	"errors"
	"math"
	"strings"
	"testing"
)

type pricingFixture struct {
	prices map[string]ModelPrice
	calls  int
	err    error
}

func (p *pricingFixture) Prices() map[string]ModelPrice { return p.prices }
func (p *pricingFixture) SetPriceAndSave(model string, price ModelPrice) error {
	p.calls++
	if p.err != nil {
		return p.err
	}
	p.prices[model] = price
	return nil
}

func TestPriceValidationOrderAndSaveFailure(t *testing.T) {
	st := &pricingFixture{prices: map[string]ModelPrice{}}
	m := NewManagement(st)
	for _, tc := range []struct {
		model   string
		price   ModelPrice
		message string
	}{
		{strings.Repeat("m", 201), ModelPrice{Currency: "CNY"}, "币种必须为 USD"},
		{strings.Repeat("m", 201), ModelPrice{Currency: "USD", Input: math.NaN()}, "每百万 Token 单价"},
		{strings.Repeat("m", 201), ModelPrice{Currency: "USD", Source: strings.Repeat("s", 1001)}, "价格来源过长"},
		{strings.Repeat("m", 201), ModelPrice{Currency: "USD"}, "模型 ID 过长"},
		{"m", ModelPrice{Currency: "USD", Output: math.Inf(1)}, "每百万 Token 单价"},
		{"m", ModelPrice{Currency: "USD", Cached: -1}, "每百万 Token 单价"},
	} {
		err := m.SetPrice(tc.model, tc.price)
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatal(err)
		}
	}
	if st.calls != 0 {
		t.Fatal("invalid price reached persistence")
	}
	p := ModelPrice{Currency: "USD", Input: 1e6, Cached: 0, Source: "fixture"}
	st.err = errors.New("disk failure")
	err := m.SetPrice("m", p)
	if !errors.Is(err, ErrPriceSave) || !errors.Is(err, st.err) || len(st.prices) != 0 {
		t.Fatal("failure classification or publication changed", err)
	}
	st.err = nil
	if err := m.SetPrice("m", p); err != nil {
		t.Fatal(err)
	}
	if m.Prices()["m"] != p {
		t.Fatal("stored value changed")
	}
	st.prices["m"] = ModelPrice{Currency: "USD"}
	if m.Prices()["m"].Input != 0 {
		t.Fatal("management cached stale prices")
	}
}
