package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/store"
)

const nonJSONRequest = `{"model":"test-model","messages":[{"role":"user","content":"hi"}],"input":"hi","max_tokens":100}`
const nonJSONRecovery = `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`

func nonJSONResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestNonJSONResponseRejectedAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for name, body := range map[string]string{"text": "PRIVATE_UPSTREAM_DETAIL", "html": "<html>PRIVATE_UPSTREAM_DETAIL</html>", "empty": "", "whitespace": " \n\t", "truncated": `{"choices":[`, "trailing": `{} {}`} {
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
					t.Fatalf("invalid response counted as success: %+v", log)
				}
				if a := s.Store.Account("a"); a.Status != "ok" || a.Failures != 0 {
					t.Fatal("temporary upstream error damaged account health")
				}
				if s.Store.PoolSnapshot()["a"].InFlight != 0 {
					t.Fatal("account lease leaked")
				}
			})
		}
	}
}

func TestNonJSONResponsePreservesValidReplies(t *testing.T) {
	for name, message := range map[string]string{
		"text":      `{"role":"assistant","content":"11128 cc_version=42"}`,
		"tool_only": `{"role":"assistant","content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"read","arguments":"{\"id\":9007199254740993}"}}]}`,
		"refusal":   `{"role":"assistant","content":null,"refusal":"Cannot comply."}`,
		"reasoning": `{"role":"assistant","content":"","reasoning_content":"thinking"}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := fmt.Sprintf(" \n{\"choices\":[{\"message\":%s,\"finish_reason\":\"provider_extension\"}],\"extension\":{\"id\":9007199254740993},\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n", message)
			calls := 0
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				calls++
				return nonJSONResponse(r, body), nil
			})
			w := responseRequest(s, key, "/v1/chat/completions", nonJSONRequest)
			if w.Code != 200 || w.Body.String() != body || calls != 1 {
				t.Fatalf("valid reply changed or retried: status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
			}
			if s.Store.Stats().FailedRequests != 0 || !s.Store.Logs(1)[0].Usage.Known {
				t.Fatal("valid reply accounting changed")
			}
		})
	}
}

type nonJSONWriter struct {
	*httptest.ResponseRecorder
	committed bool
}

func (w *nonJSONWriter) WriteHeader(status int) {
	w.committed = true
	w.ResponseRecorder.WriteHeader(status)
}
func (w *nonJSONWriter) Write(b []byte) (int, error) {
	w.committed = true
	return w.ResponseRecorder.Write(b)
}

func TestNonJSONResponseUsesExistingFailover(t *testing.T) {
	restore := waitRetry
	waits := 0
	waitRetry = func(context.Context, time.Duration) error { waits++; return nil }
	t.Cleanup(func() { waitRetry = restore })
	w := &nonJSONWriter{ResponseRecorder: httptest.NewRecorder()}
	var calls int
	var credentials []string
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls++
		credentials = append(credentials, r.Header.Get("Authorization"))
		if w.committed {
			t.Fatal("response committed before failover completed")
		}
		if calls == 1 {
			return nonJSONResponse(r, "broken JSON"), nil
		}
		return nonJSONResponse(r, nonJSONRecovery), nil
	})
	s.Store.UpdateSettings(func(st *store.Settings) { st.Pool.RateCooldownSec = 60 })
	s.Store.UpsertAccount(&codearts.Account{ID: "b", Name: "second", Enabled: true, Status: "ok", Session: &codearts.Session{AccessKey: "ak-b", SecretKey: "sk-b", SecurityToken: "st-b", ExpiresAt: time.Now().Add(time.Hour)}})
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(nonJSONRequest))
	r.Header.Set("Authorization", "Bearer "+key)
	s.Handler().ServeHTTP(w, r)
	if calls != 2 || waits != 1 || credentials[0] == credentials[1] || w.Code != 200 || w.Body.String() != nonJSONRecovery {
		t.Fatalf("failover: calls=%d waits=%d status=%d body=%s", calls, waits, w.Code, w.Body.String())
	}
	if log := s.Store.Logs(1)[0]; log.Attempts != 2 || log.Status != 200 || !log.UsageIncomplete || log.Usage.InputTokens != 3 || log.Usage.OutputTokens != 1 {
		t.Fatalf("attempt accounting: %+v", log)
	} else if len(log.AttemptDetails) != 2 || log.AttemptDetails[0].Account == log.AttemptDetails[1].Account || log.AttemptDetails[0].Usage.Known || !log.AttemptDetails[1].Usage.Known {
		t.Fatalf("failover must use a different account and preserve unknown usage: %+v", log.AttemptDetails)
	}
	for _, a := range s.Store.Accounts() {
		if a.Status != "ok" || a.Failures != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
			t.Fatal("failover damaged account health or leaked lease")
		}
	}
}

func TestNonJSONResponseHonorsRetryLimit(t *testing.T) {
	restore := waitRetry
	waitRetry = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { waitRetry = restore })
	calls := 0
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return nonJSONResponse(r, "broken JSON"), nil
	})
	s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 3 })
	w := responseRequest(s, key, "/v1/chat/completions", nonJSONRequest)
	if w.Code != 502 || calls != 3 || s.Store.Logs(1)[0].Attempts != 3 {
		t.Fatalf("retry budget changed: status=%d calls=%d", w.Code, calls)
	}
}
