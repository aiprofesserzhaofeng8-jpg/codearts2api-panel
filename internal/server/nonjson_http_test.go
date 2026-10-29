package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/store"
)

// Both hops use real localhost HTTP connections. Only the upstream address is
// redirected; production parsing, retry/backoff, response writing and accounting
// remain active. Fixtures use temporary stores and synthetic credentials only.
func TestNonJSONOverHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, first, next string
		attempts, status  int
	}{
		{"plain_text", "PRIVATE_UPSTREAM_DETAIL", "", 1, 502},
		{"html", "<html>PRIVATE_UPSTREAM_DETAIL</html>", "", 1, 502},
		{"empty", "", "", 1, 502},
		{"whitespace", " \n\t", "", 1, 502},
		{"truncated", `{"choices":[`, "", 1, 502},
		{"trailing_json", `{} {}`, "", 1, 502},
		{"recover_on_retry", "PRIVATE_UPSTREAM_DETAIL", nonJSONRecovery, 2, 200},
		{"exhaust_retries", "PRIVATE_UPSTREAM_DETAIL", "PRIVATE_UPSTREAM_DETAIL", 3, 502},
		{"valid_tool_only", ` {"choices":[{"message":{"content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"read","arguments":"{\"n\":9007199254740993}"}}]},"finish_reason":"tool_calls"}],"extension":9007199254740993} `, "", 1, 200},
		{"valid_refusal", `{"choices":[{"message":{"content":null,"refusal":"Cannot comply."},"finish_reason":"stop"}]}`, "", 1, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := tc.first
				if calls.Add(1) > 1 {
					body = tc.next
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()
			target, _ := url.Parse(upstream.URL)
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			defer transport.CloseIdleConnections()
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				cloned := r.Clone(r.Context())
				cloned.URL.Scheme, cloned.URL.Host = target.Scheme, target.Host
				cloned.Host = target.Host
				return transport.RoundTrip(cloned)
			})
			// Disable cooldown only in this single-account fixture so the same
			// synthetic credential can retry; keep real backoff and attempt limits.
			s.Store.UpdateSettings(func(st *store.Settings) {
				st.MaxRetries = tc.attempts
				st.Pool.RateCooldownSec = 0
			})
			gateway := httptest.NewServer(s.Handler())
			defer gateway.Close()
			client := gateway.Client()
			client.Timeout = 10 * time.Second
			req, _ := http.NewRequest("POST", gateway.URL+"/v1/chat/completions", strings.NewReader(nonJSONRequest))
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Content-Type", "application/json")
			start := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			log := s.Store.Logs(1)[0]
			t.Logf("upstream=200 downstream=%d calls=%d log_status=%d failed_requests=%d elapsed=%s body=%s", resp.StatusCode, calls.Load(), log.Status, s.Store.Stats().FailedRequests, time.Since(start).Round(time.Millisecond), body)
			if resp.StatusCode != tc.status || int(calls.Load()) != tc.attempts || !json.Valid(body) || strings.Contains(string(body), "PRIVATE_UPSTREAM_DETAIL") {
				t.Fatal("unexpected status, attempts, malformed output or upstream body leak")
			}
			if tc.status == 200 {
				want := tc.first
				if tc.attempts > 1 {
					want = tc.next
				}
				if string(body) != want || s.Store.Stats().FailedRequests != 0 {
					t.Fatal("valid reply altered, contaminated by failed attempt or counted as failure")
				}
			} else if s.Store.Stats().FailedRequests != 1 {
				t.Fatal("failed request counted as success")
			}
			if log.Status != tc.status || log.Attempts != tc.attempts {
				t.Fatal("attempt/status accounting mismatch")
			}
			if a := s.Store.Account("a"); a.Failures != 0 || a.Status != "ok" || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
				t.Fatal("account health changed or lease leaked")
			}
			if tc.attempts > 1 && time.Since(start) < time.Duration(tc.attempts-1)*time.Second {
				t.Fatal(fmt.Sprintf("existing retry backoff was skipped: %s", time.Since(start)))
			}
		})
	}
}
