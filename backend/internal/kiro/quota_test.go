package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codearts2api/internal/accounts"
)

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
					w.Write([]byte(`{"usageBreakdownList":[{"currentUsageWithPrecision":2,"usageLimitWithPrecision":10}]}`))
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
				if _, e = c.Credit(context.Background(), normalized); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}
