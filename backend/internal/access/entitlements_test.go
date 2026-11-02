package access

import (
	"math"
	"testing"
)

func TestCreditDecimalExactAndBounds(t *testing.T) {
	for _, tt := range []struct {
		S    string
		Want int64
	}{{"0", 0}, {"1.00000001", 100000001}, {"-0.00000001", -1}, {"1000000", MaxCredits}, {"0.1", 10000000}} {
		got, err := ParseCredits(tt.S)
		if err != nil || got != tt.Want {
			t.Fatal(tt, got, err)
		}
	}
	for _, s := range []string{"NaN", "1e3", "1.000000001", "1000000.00000001", "-", "1..2", "+2", "1\n2"} {
		if _, err := ParseCredits(s); err == nil {
			t.Fatal("invalid decimal accepted", s)
		}
	}
	for _, n := range []float64{math.NaN(), math.Inf(1), -1} {
		if _, err := CostCredits(n); err == nil {
			t.Fatal("invalid cost accepted")
		}
	}
}
