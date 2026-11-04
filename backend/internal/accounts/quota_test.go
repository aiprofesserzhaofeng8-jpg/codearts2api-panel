package accounts

import (
	"encoding/json"
	"math"
	"testing"
)

func TestQuotaMetricsKeepUnknownZeroAndExplicitPrecision(t *testing.T) {
	var o map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"precise":0,"legacy":9,"empty":null,"decimal":"2.5"}`), &o)
	n, e := QuotaNumber(o, "precise", "legacy")
	if e != nil || n == nil || *n != 0 {
		t.Fatal("known zero lost")
	}
	n, e = QuotaNumber(o, "empty")
	if e != nil || n != nil {
		t.Fatal("null became zero")
	}
	n, e = QuotaNumber(o, "decimal")
	if e != nil || n == nil || *n != 2.5 {
		t.Fatal("decimal representation lost")
	}
	for _, s := range []string{`"NaN"`, `"Inf"`, `true`, `{}`, `""`} {
		if _, e = QuotaNumber(map[string]json.RawMessage{"x": json.RawMessage(s)}, "x"); e == nil {
			t.Fatal("invalid numeric metric accepted")
		}
	}
	zero, total := 0.0, 20.0
	w := QuotaWindow{Used: &zero, Total: &total}
	if !w.Complete() || w.Utilization == nil || *w.Utilization != 0 || *w.Remaining != 20 {
		t.Fatal("incorrect zero usage arithmetic")
	}
	unknown := QuotaWindow{}
	if unknown.Complete() {
		t.Fatal("empty window became measured")
	}
	for _, v := range []float64{-1, math.Inf(1), math.NaN()} {
		w := QuotaWindow{Used: &v}
		if w.Complete() {
			t.Fatal("invalid metric accepted")
		}
	}
	info := CreditInfo{Windows: []QuotaWindow{{Unit: "credits", Used: w.Used, Total: w.Total, Remaining: w.Remaining}, {Unit: "percent", Utilization: w.Utilization}, {Unit: "credits", Unlimited: true}}}
	info.SummarizeCredits()
	if info.Total != 20 || info.Used != 0 || info.Remaining != 20 {
		t.Fatal("subscription percentage or unlimited pool became credits")
	}
}
