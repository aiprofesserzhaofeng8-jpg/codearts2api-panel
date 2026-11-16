package commandcode

import (
	"codearts2api/internal/accounts"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonthlyQuotaAndPartialFailure(t *testing.T) {
	for _, mode := range []string{"complete", "unknown", "failure", "unauthorized", "negative", "invalid_period"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/alpha/whoami" && r.URL.Query().Get("orgId") != "organization" {
					t.Error("lost organization")
				}
				switch r.URL.Path {
				case "/alpha/whoami":
					io.WriteString(w, `{"org":{"id":"organization"}}`)
				case "/alpha/billing/credits":
					io.WriteString(w, `{"credits":{"monthlyCredits":34.9,"purchasedCredits":10}}`)
				case "/alpha/billing/subscriptions":
					if mode == "failure" {
						w.WriteHeader(500)
						return
					}
					if mode == "unauthorized" {
						w.WriteHeader(401)
						return
					}
					if mode == "invalid_period" {
						io.WriteString(w, `{"data":{"currentPeriodStart":"bad","currentPeriodEnd":"bad"}}`)
						return
					}
					io.WriteString(w, `{"data":{"currentPeriodStart":"2026-11-01T00:00:00Z","currentPeriodEnd":"2026-12-01T00:00:00Z"}}`)
				case "/alpha/usage/summary":
					if r.URL.Query().Get("since") != "2026-11-01T00:00:00Z" {
						t.Error("wrong billing period")
					}
					if mode == "unknown" {
						io.WriteString(w, `{"totalCost":99}`)
					} else if mode == "negative" {
						io.WriteString(w, `{"totalMonthlyCredits":-1}`)
					} else {
						io.WriteString(w, `{"totalMonthlyCredits":35.1,"totalCost":99}`)
					}
				default:
					t.Error(r.URL.Path)
				}
			}))
			defer s.Close()
			c := New()
			c.Origin = s.URL
			b, err := c.Credit(context.Background(), rawKey())
			if mode == "unauthorized" {
				if err == nil {
					t.Fatal("auth failure hidden")
				}
				return
			}
			var q accounts.CreditInfo
			json.Unmarshal(b, &q)
			if err != nil || !q.Available || len(q.Windows) != 2 {
				t.Fatal(string(b), err)
			}
			monthly := q.Windows[0]
			if mode == "complete" {
				if monthly.Total == nil || *monthly.Total != 70 || *monthly.Used != 35.1 || monthly.ResetsAt != 1796083200 || calls != 4 {
					t.Fatal(monthly, calls)
				}
			} else {
				if monthly.Total != nil || monthly.Used != nil || monthly.ResetsAt != 0 || !strings.Contains(q.Reason, "月度") {
					t.Fatal("partial response fabricated", string(b))
				}
			}
		})
	}
}
