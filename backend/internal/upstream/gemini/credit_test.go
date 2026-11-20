package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"codearts2api/internal/accounts"
)

func TestCLIQuotaKnownZeroAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		body            string
		fail, available bool
	}{
		{`{"buckets":[{"modelId":"gemini-3-flash","tokenType":"REQUESTS","remainingAmount":"0","remainingFraction":0,"resetTime":"2026-12-01T00:00:00Z"}]}`, false, true},
		{`{}`, false, false}, {`{"buckets":[]}`, false, false}, {`{"buckets":[{"modelId":"unknown"}]}`, false, false},
		{`{"buckets":[{"remainingFraction":1.5}]}`, true, false}, {`{"buckets":[{"remainingAmount":"NaN"}]}`, true, false}, {`{"buckets":[{"remainingAmount":-1}]}`, true, false}, {`{"buckets":[{"remainingAmount":0,"resetTime":"invalid"}]}`, true, false},
	} {
		calls := 0
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/v1internal:retrieveUserQuota" || r.Method != "POST" {
				t.Error("wrong quota route")
			}
			var request map[string]string
			json.NewDecoder(r.Body).Decode(&request)
			if request["project"] != "project-1" {
				t.Error("wrong project")
			}
			io.WriteString(w, tc.body)
		}))
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
		c := New()
		c.CLIBase = up.URL
		b, err := c.Credit(context.Background(), cliFixture())
		up.Close()
		if calls != 1 || (err != nil) != tc.fail {
			t.Fatal(tc, err)
		}
		if tc.fail {
			continue
		}
		var info accounts.CreditInfo
		if json.Unmarshal(b, &info) != nil || info.Available != tc.available {
			t.Fatal(string(b))
		}
		if tc.available {
			w := info.Windows[0]
			if w.Remaining == nil || *w.Remaining != 0 || w.Utilization == nil || *w.Utilization != 100 || w.Total != nil || w.Used != nil || w.ResetsAt == 0 {
				t.Fatal(w)
			}
		}
	}
}
