package server

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPersistenceFailureStopsInferenceWithoutChangingValidResponse(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return responseUpstream(r, responsesTextFixture), nil
			})
			s.Reg.MarkStreamOnly("test-model")
			if err := s.Store.Save(); err != nil {
				t.Fatal(err)
			}
			journal := s.Store.Path() + ".usage.jsonl"
			if err := os.Mkdir(journal, 0o700); err != nil {
				t.Fatal(err)
			}
			body := `{"model":"test-model","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`
			if path == "/v1/responses" {
				body = `{"model":"test-model","input":"hi"}`
			}
			first := responseRequest(s, key, path, body)
			if first.Code != 200 || !strings.Contains(first.Body.String(), "Hello") {
				t.Fatalf("valid upstream response was damaged: %d %s", first.Code, first.Body.String())
			}
			blocked := responseRequest(s, key, path, body)
			if blocked.Code != 503 || calls.Load() != 1 {
				t.Fatalf("storage failure did not stop inference: status=%d calls=%d", blocked.Code, calls.Load())
			}
			if bytes.Contains(blocked.Body.Bytes(), []byte(s.Store.Path())) || s.Store.Account("a").Failures != 0 {
				t.Fatal("local storage failure exposed a path or penalized the upstream account")
			}
			if s.Store.Keys()[0].BudgetUsage.Tokens != 16 {
				t.Fatal("real usage was dropped after storage failure")
			}
			if err := os.Remove(journal); err != nil {
				t.Fatal(err)
			}
			if err := s.Store.Save(); err != nil {
				t.Fatal(err)
			}
			if again := responseRequest(s, key, path, body); again.Code != 200 || calls.Load() != 2 {
				t.Fatalf("inference did not recover after durable save: %d", again.Code)
			}
		})
	}
}
