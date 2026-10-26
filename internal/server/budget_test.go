package server

import (
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBudgetsAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return responseUpstream(r, responsesTextFixture), nil
			})
			s.Reg.MarkStreamOnly("test-model")
			if err := s.Store.SetKeyBudgetAndSave(key, store.KeyBudget{MaxRequests: 1}, false); err != nil {
				t.Fatal(err)
			}
			body := `{"model":"test-model","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`
			if path == "/v1/responses" {
				body = `{"model":"test-model","input":"hi"}`
			}
			if w := responseRequest(s, key, path, body); w.Code != 200 {
				t.Fatalf("first=%d %s", w.Code, w.Body.String())
			}
			if w := responseRequest(s, key, path, body); w.Code != 402 {
				t.Fatalf("over budget=%d", w.Code)
			}
			if calls.Load() != 1 {
				t.Fatal("budget reached upstream")
			}
			used := s.Store.Keys()[0].BudgetUsage
			if used.Requests != 1 || used.Tokens != 16 {
				t.Fatalf("wrong usage: %+v", used)
			}
			req := httptest.NewRequest("GET", "/v1/capabilities", nil)
			req.Header.Set("Authorization", "Bearer "+key)
			rw := httptest.NewRecorder()
			s.Handler().ServeHTTP(rw, req)
			if rw.Code != 200 {
				t.Fatal("metadata blocked by inference budget")
			}
		})
	}
}
func TestBudgetStopsSummaryRetry(t *testing.T) {
	var calls atomic.Int32
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return responseUpstream(r, summaryFixture("partial", "length", 10, 8)), nil
	})
	s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}})
	if err := s.Store.SetKeyBudgetAndSave(key, store.KeyBudget{MaxTokens: 15}, false); err != nil {
		t.Fatal(err)
	}
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":"You are a context summarization assistant."},{"role":"user","content":"history"}]}`)
	if w.Code != 402 || calls.Load() != 1 {
		t.Fatalf("unbounded summary budget: status=%d calls=%d %s", w.Code, calls.Load(), w.Body.String())
	}
	if s.Store.Account("a").Failures != 0 {
		t.Fatal("budget poisoned account")
	}
	if u := s.Store.Keys()[0].BudgetUsage; u.Tokens != 18 || u.Requests != 1 || u.USD <= 0 {
		t.Fatalf("lost summary billing: %+v", u)
	}
}
