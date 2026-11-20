package trae

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// This shape comes from a real response: one capability-only entitlement and
// one 500-credit pack, both with empty usage. No real IDs or tokens are retained.
const creditSummaryFixture = `{
 "is_credits_billing":true,"is_dollar_usage_billing":false,
 "usage_summary":{"total_amount":500,"consumed_amount":0,"consumption_ratio":0},
 "user_entitlement_pack_list":[
  {"entitlement_base_info":{"quota":{"enable_solo_agent":true}},"usage":{}},
  {"entitlement_base_info":{"quota":{"credits_limit":500}},"usage":{}}
 ]
}`

func TestCreditSummaryWithCapabilityOnlyPack(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != CreditPath {
			t.Error("wrong quota path")
		}
		io.WriteString(w, creditSummaryFixture)
	})
	raw, err := c.Credit(context.Background(), fixtureCredentials())
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Available              bool `json:"available"`
		Total, Used, Remaining float64
		Reason                 string
	}
	if err = json.Unmarshal(raw, &info); err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Total != 500 || info.Used != 0 || info.Remaining != 500 || info.Reason != "" {
		t.Fatalf("summary quota lost: %+v", info)
	}
}
func TestCreditSummaryUnitsMissingValuesAndPrecedence(t *testing.T) {
	cases := []struct {
		name, body             string
		available              bool
		total, used, remaining float64
	}{
		{"summary is authoritative", `{"is_credits_billing":true,"usage_summary":{"total_amount":500,"consumed_amount":100},"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":2000}},"usage":{"credits_amount":50}}]}`, true, 500, 100, 400},
		{"fractional", `{"is_credits_billing":true,"usage_summary":{"total_amount":500.5,"consumed_amount":1.25}}`, true, 500.5, 1.25, 499.25},
		{"explicit zero", `{"is_credits_billing":true,"usage_summary":{"total_amount":0,"consumed_amount":0}}`, true, 0, 0, 0},
		{"overdrawn", `{"is_credits_billing":true,"usage_summary":{"total_amount":5,"consumed_amount":6}}`, true, 5, 6, 0},
		{"missing consumed", `{"is_credits_billing":true,"usage_summary":{"total_amount":500}}`, false, 0, 0, 0},
		{"missing total", `{"is_credits_billing":true,"usage_summary":{"consumed_amount":0}}`, false, 0, 0, 0},
		{"unknown units", `{"usage_summary":{"total_amount":500,"consumed_amount":0}}`, false, 0, 0, 0},
		{"dollar billing", `{"is_credits_billing":false,"is_dollar_usage_billing":true,"usage_summary":{"total_amount":500,"consumed_amount":0}}`, false, 0, 0, 0},
		{"mixed billing", `{"is_credits_billing":true,"is_dollar_usage_billing":true,"usage_summary":{"total_amount":500,"consumed_amount":0}}`, false, 0, 0, 0},
		{"unlimited sentinel", `{"is_credits_billing":true,"usage_summary":{"total_amount":-1,"consumed_amount":0}}`, false, 0, 0, 0},
		{"unlimited summary overrides finite pack", `{"is_credits_billing":true,"usage_summary":{"total_amount":-1,"consumed_amount":0},"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":500}},"usage":{"credits_amount":0}}]}`, false, 0, 0, 0},
		{"wrapped summary", `{"code":0,"data":{"is_credits_billing":true,"usage_summary":{"total_amount":500,"consumed_amount":0}}}`, true, 500, 0, 500},
		{"legacy packs", `{"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":100}},"usage":{"credits_amount":0.25}}]}`, true, 100, .25, 99.75},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := parseCreditInfo([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			var info struct {
				Available              bool `json:"available"`
				Total, Used, Remaining float64
			}
			if json.Unmarshal(raw, &info) != nil {
				t.Fatal("bad normalized result")
			}
			if info.Available != tc.available || info.Total != tc.total || info.Used != tc.used || info.Remaining != tc.remaining {
				t.Fatalf("got %+v", info)
			}
		})
	}
}
