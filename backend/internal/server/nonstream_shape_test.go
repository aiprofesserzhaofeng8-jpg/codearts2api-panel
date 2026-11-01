package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/store"
)

func TestNonStreamingInvalidEnvelopeAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for name, body := range map[string]string{
			"null":            `null`,
			"empty_choices":   `{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1}}`,
			"missing_message": `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`,
			"error":           `{"error":{"message":"PRIVATE_UPSTREAM_DETAIL","type":"upstream_error"}}`,
		} {
			t.Run(path+"/"+name, func(t *testing.T) {
				calls := 0
				s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
					calls++
					return nonJSONResponse(r, body), nil
				})
				s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 1 })
				w := responseRequest(s, key, path, nonJSONRequest)
				if w.Code != 502 || calls != 1 || strings.Contains(w.Body.String(), "PRIVATE_UPSTREAM_DETAIL") {
					t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
				}
				log := s.Store.Logs(1)[0]
				if log.Status != 502 || log.Attempts != 1 || log.Usage.Known || s.Store.Stats().FailedRequests != 1 {
					t.Fatalf("invalid response counted as success or billed usage: %+v", log)
				}
				if a := s.Store.Account("a"); a.Status != "ok" || a.Failures != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
					t.Fatal("malformed upstream reply damaged credential health or leaked lease")
				}
			})
		}
	}
}

func TestNonStreamingInvalidEnvelopeUsesExistingFailover(t *testing.T) {
	restore := waitRetry
	waits := 0
	waitRetry = func(context.Context, time.Duration) error { waits++; return nil }
	t.Cleanup(func() { waitRetry = restore })
	w := &nonJSONWriter{ResponseRecorder: httptest.NewRecorder()}
	calls := 0
	var credentials []string
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls++
		credentials = append(credentials, r.Header.Get("Authorization"))
		if w.committed {
			t.Fatal("invalid envelope committed before failover")
		}
		if calls == 1 {
			return nonJSONResponse(r, `{"error":{"message":"PRIVATE_UPSTREAM_DETAIL"}}`), nil
		}
		return nonJSONResponse(r, nonJSONRecovery), nil
	})
	s.Store.UpdateSettings(func(st *store.Settings) { st.Pool.RateCooldownSec = 60 })
	s.Store.UpsertAccount(&codearts.Account{ID: "b", Name: "second", Enabled: true, Status: "ok", Session: &codearts.Session{AccessKey: "ak-b", SecretKey: "sk-b", SecurityToken: "st-b", ExpiresAt: time.Now().Add(time.Hour)}})
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(nonJSONRequest))
	r.Header.Set("Authorization", "Bearer "+key)
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != nonJSONRecovery || calls != 2 || waits != 1 || credentials[0] == credentials[1] {
		t.Fatalf("failover changed: status=%d calls=%d waits=%d body=%s", w.Code, calls, waits, w.Body.String())
	}
	log := s.Store.Logs(1)[0]
	if log.Status != 200 || log.Attempts != 2 || !log.UsageIncomplete || len(log.AttemptDetails) != 2 || log.AttemptDetails[0].Usage.Known || !log.AttemptDetails[1].Usage.Known {
		t.Fatalf("invalid reply usage silently counted as known: %+v", log)
	}
	for _, a := range s.Store.Accounts() {
		if a.Status != "ok" || a.Failures != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
			t.Fatal("protocol failure damaged credential health or leaked lease")
		}
	}
}
