package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codearts2api/internal/accounts"
)

func TestQuotaCreditResourceUsesInvocationUnit(t *testing.T) {
	// GetUsageLimits reports semantic Credits as resourceType=CREDIT while its
	// transport unit is INVOCATIONS. Match the observed wire shape, not a DTO
	// that has already discarded resource metadata.
	var o map[string]json.RawMessage
	if e := json.Unmarshal([]byte(`{"subscriptionInfo":{"subscriptionTitle":"KIRO PRO MAX"},"nextDateReset":1796000000,"usageBreakdownList":[{"currency":"USD","currentUsage":282,"currentUsageWithPrecision":282.49,"displayName":"Credit","displayNamePlural":"Credits","resourceType":"CREDIT","unit":"INVOCATIONS","usageLimit":5000,"usageLimitWithPrecision":5000.0,"freeTrialInfo":null}]}`), &o); e != nil {
		t.Fatal(e)
	}
	b, e := parseQuota(o, 1793990000)
	if e != nil {
		t.Fatal(e)
	}
	var v accounts.CreditInfo
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if !v.Available || len(v.Windows) != 1 || v.Unit != "credits" || v.Total != 5000 || v.Used != 282.49 || math.Abs(v.Remaining-4717.51) > 1e-9 || v.Windows[0].ResetsAt != 1796000000 {
		t.Fatalf("valid Credits resource was dropped or misinterpreted: %+v", v)
	}
}

func TestQuotaResourceSelectionRetainsUnitsAndUnknowns(t *testing.T) {
	for _, tc := range []struct {
		name, rows string
		available  bool
		remaining  float64
	}{
		{"credit after tokens", `[{"resourceType":"TOKENS","unit":"TOKENS","currentUsage":50,"usageLimit":1000},{"resourceType":"CREDIT","unit":"INVOCATIONS","currentUsageWithPrecision":2.25,"usageLimitWithPrecision":100}]`, true, 97.75},
		{"credit before tokens", `[{"resourceType":"CREDIT","unit":"INVOCATIONS","currentUsage":10,"usageLimit":50},{"resourceType":"TOKENS","unit":"TOKENS","currentUsage":5,"usageLimit":1000}]`, true, 40},
		{"explicit credit preferred", `[{"unit":"CREDITS","currentUsage":1,"usageLimit":999},{"resourceType":"CREDIT","unit":"COUNT","currentUsage":2,"usageLimit":100}]`, true, 98},
		{"agentic generic unit", `[{"resourceType":"AGENTIC_REQUEST","unit":"INVOCATIONS","currentUsage":0,"usageLimit":50}]`, true, 50},
		{"credit label", `[{"displayNamePlural":"Credits","unit":"INVOCATIONS","currentUsage":3,"usageLimit":10}]`, true, 7},
		{"known zero", `[{"resourceType":"CREDIT","unit":"INVOCATIONS","currentUsageWithPrecision":0,"currentUsage":9,"usageLimitWithPrecision":0}]`, true, 0},
		{"active trial", `[{"resourceType":"CREDIT","unit":"INVOCATIONS","currentUsage":0,"usageLimit":0,"freeTrialInfo":{"currentUsageWithPrecision":1.5,"usageLimitWithPrecision":50,"freeTrialStatus":"ACTIVE"}}]`, true, 48.5},
		{"missing counters", `[{"resourceType":"CREDIT","unit":"INVOCATIONS"}]`, false, 0},
		{"ambiguous invocations", `[{"unit":"INVOCATIONS","currentUsage":1,"usageLimit":10}]`, false, 0},
		{"other resource generic unit", `[{"resourceType":"TOKENS","unit":"INVOCATIONS","currentUsage":1,"usageLimit":10}]`, false, 0},
		{"other display label", `[{"displayNamePlural":"Tokens","currentUsage":1,"usageLimit":10}]`, false, 0},
		{"contradictory token unit", `[{"resourceType":"CREDIT","unit":"TOKENS","currentUsage":1,"usageLimit":10}]`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var o map[string]json.RawMessage
			if e := json.Unmarshal([]byte(`{"usageBreakdownList":`+tc.rows+`}`), &o); e != nil {
				t.Fatal(e)
			}
			b, e := parseQuota(o, 1)
			if e != nil {
				t.Fatal(e)
			}
			var info accounts.CreditInfo
			if e = json.Unmarshal(b, &info); e != nil {
				t.Fatal(e)
			}
			if info.Available != tc.available || math.Abs(info.Remaining-tc.remaining) > 1e-9 {
				t.Fatalf("resource selection wrong: %+v", info)
			}
		})
	}
}

func TestQuotaActiveCreditsPrecisionAndReset(t *testing.T) {
	var o map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"nextDateReset":1796000000.5,"subscriptionInfo":{"subscriptionTitle":"KIRO PRO"},"usageBreakdownList":[{"currentUsageWithPrecision":0,"currentUsage":50,"usageLimitWithPrecision":100,"freeTrialInfo":{"currentUsageWithPrecision":2.5,"usageLimitWithPrecision":20,"freeTrialStatus":"ACTIVE"},"bonuses":[{"currentUsage":1.25,"usageLimit":5,"status":"ACTIVE"},{"currentUsage":90,"usageLimit":100,"status":"EXPIRED"}]}]}`), &o)
	b, e := parseQuota(o, 1793990000)
	if e != nil {
		t.Fatal(e)
	}
	var v accounts.CreditInfo
	_ = json.Unmarshal(b, &v)
	if !v.Available || v.Total != 125 || v.Used != 3.75 || v.Remaining != 121.25 || len(v.Windows) != 3 || v.Windows[0].ResetsAt != 1796000000 || *v.Windows[0].Used != 0 {
		t.Fatalf("credits miscounted: %+v", v)
	}
	for _, s := range []string{`{}`, `{"usageBreakdownList":[{}]}`, `{"usageBreakdownList":[{"currentUsage":1,"usageLimit":10,"unit":"TOKENS"}]}`} {
		o = nil
		_ = json.Unmarshal([]byte(s), &o)
		b, e = parseQuota(o, 1)
		if e != nil {
			t.Fatal(e)
		}
		v = accounts.CreditInfo{}
		_ = json.Unmarshal(b, &v)
		if v.Available {
			t.Fatal("unknown or other unit became credits")
		}
	}
}
func TestQuotaBothRegionsAndAuthModesUseReadOnlyGET(t *testing.T) {
	for _, region := range []string{"us-east-1", "eu-central-1"} {
		for _, key := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", region, key), func(t *testing.T) {
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" || r.URL.Path != "/getUsageLimits" || r.URL.Query().Get("origin") != "AI_EDITOR" || r.URL.Query().Get("resourceType") != "AGENTIC_REQUEST" || r.Header.Get("X-Amz-Target") != "" {
						t.Error("wrong quota endpoint")
					}
					want := "Bearer quota-fixture"
					if key {
						want = "Bearer ksk_fixture"
						if r.Header.Get("TokenType") != "API_KEY" {
							t.Error("missing key marker")
						}
					}
					if r.Header.Get("Authorization") != want {
						t.Error("wrong auth")
					}
					w.Write([]byte(`{"usageBreakdownList":[{"resourceType":"CREDIT","unit":"INVOCATIONS","currentUsageWithPrecision":2,"usageLimitWithPrecision":10}]}`))
				}))
				defer up.Close()
				c := New()
				c.Regions[region] = Region{Runtime: up.URL}
				a := Credentials{AccessToken: "quota-fixture", RefreshToken: "refresh-fixture", AuthMethod: "social", ProfileARN: "arn:aws:codewhisperer:" + region + ":123456789012:profile/fixture", Region: region, ExpiresAt: time.Now().Add(time.Hour).Unix()}
				if key {
					a = Credentials{APIKey: "ksk_fixture", Region: region}
				}
				raw, _ := json.Marshal(a)
				normalized, _, e := c.Normalize(raw)
				if e != nil {
					t.Fatal(e)
				}
				result, e := c.Credit(context.Background(), normalized)
				if e != nil {
					t.Fatal(e)
				}
				var info accounts.CreditInfo
				if e = json.Unmarshal(result, &info); e != nil || !info.Available || info.Remaining != 8 {
					t.Fatal("native Credits resource unavailable", e, info.Reason)
				}
			})
		}
	}
}
