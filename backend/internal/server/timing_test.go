package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/store"
)

func TestFirstUsefulDeltaAndAttemptTiming(t *testing.T) {
	for _, delta := range []string{`{"content":"text"}`, `{"reasoning_content":"think"}`, `{"reasoning":"think"}`, `{"refusal":"no"}`, `{"tool_calls":[{"id":"call_1","function":{"name":"read","arguments":""}}]}`, `{"tool_calls":[{"function":{"arguments":"9007199254740993"}}]}`} {
		t.Run(delta, func(t *testing.T) {
			ctx := withUsageCapture(context.Background(), "chat")
			configureInference(ctx, time.Now().Add(-20*time.Millisecond), true)
			resetUsage(ctx)
			c := ctx.Value(usageContextKey{}).(*usageCapture)
			c.attemptStarted = time.Now().Add(-10 * time.Millisecond)
			for _, empty := range []string{`{"role":"assistant"}`, `{}`, `{"content":"","reasoning_content":null}`, `{"tool_calls":[{"index":0}]}`} {
				captureUsage(ctx, []byte(`{"choices":[{"delta":`+empty+`}]}`))
			}
			if c.firstTokenMS != nil || c.attemptFirstMS != nil {
				t.Fatal("empty/role-only frame counted as useful output")
			}
			raw := []byte(`{"choices":[{"delta":` + delta + `}]}`)
			original := string(raw)
			captureUsage(ctx, raw)
			first := c.firstTokenMS
			if first == nil || *first < 20 || c.attemptFirstMS == nil || *c.attemptFirstMS < 10 || string(raw) != original {
				t.Fatal("missing/invalid first delta time, or payload changed")
			}
			captureUsage(ctx, []byte(`{"choices":[{"delta":{"content":"later"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`))
			finishAttempt(ctx, nil)
			d := requestAttemptDetails(ctx)[0]
			if c.firstTokenMS != first || d.Status != 200 || d.DurationMS == nil || *d.DurationMS < *d.FirstTokenMS || d.FinishReason != "stop" || !d.Usage.Known || d.Usage.InputTokens != 7 {
				t.Fatalf("incorrect completion timing/usage: %+v", d)
			}
			finishAttempt(ctx, errors.New("late error"))
			if c.attemptStatus != 200 {
				t.Fatal("completed attempt overwritten")
			}
			resetUsage(ctx)
			if c.attemptFirstMS != nil || c.firstTokenMS != first {
				t.Fatal("retry did not reset only its own first delta time")
			}
			finishAttempt(ctx, context.Canceled)
			details := requestAttemptDetails(ctx)
			if len(details) != 2 || details[0].Status != 200 || details[1].ErrorKind != "canceled" || details[1].DurationMS == nil || details[1].FirstTokenMS != nil {
				t.Fatalf("attempts mixed together: %+v", details)
			}
		})
	}
	ctx := withUsageCapture(context.Background(), "chat")
	configureInference(ctx, time.Now(), false)
	resetUsage(ctx)
	captureUsage(ctx, []byte(`{"choices":[{"message":{"content":"buffered"},"finish_reason":"stop"}]}`))
	if ctx.Value(usageContextKey{}).(*usageCapture).firstTokenMS != nil {
		t.Fatal("buffered result invented streaming timing")
	}
}

func TestRejectedInferenceLoggedWithoutUsageOrAccountPenalty(t *testing.T) {
	for _, protocol := range []string{"chat", "messages", "responses"} {
		for _, invalid := range []string{"malformed", "empty", "model", "thinking"} {
			if invalid == "thinking" && protocol != "messages" {
				continue
			}
			t.Run(protocol+"/"+invalid, func(t *testing.T) {
				s, key := responseTestServer(t, func(*http.Request) (*http.Response, error) {
					t.Error("invalid request reached upstream")
					return nil, errors.New("unreachable")
				})
				if err := s.Store.SetKeyBudgetAndSave(key, store.KeyBudget{MaxRequests: 1, MaxTokens: 64}, false); err != nil {
					t.Fatal(err)
				}
				before := s.Store.Account("a")
				body := map[string]any{"model": "test-model", "messages": []map[string]string{{"role": "user", "content": "hi"}}, "max_tokens": 64}
				path := "/v1/chat/completions"
				if protocol == "messages" {
					path = "/v1/messages"
				} else if protocol == "responses" {
					path = "/v1/responses"
					delete(body, "messages")
					body["input"] = "hi"
				}
				want := 400
				switch invalid {
				case "model":
					body["model"], want = "unknown-model", 404
				case "empty":
					delete(body, "messages")
					delete(body, "input")
				case "thinking":
					body["thinking"] = map[string]string{"type": "adaptive"}
				}
				raw, _ := json.Marshal(body)
				if invalid == "malformed" {
					raw = []byte(`{"model":`)
				}
				w := responseRequest(s, key, path, string(raw))
				logs := s.Store.Logs(10)
				if w.Code != want || !strings.Contains(w.Body.String(), "invalid_request_error") || len(logs) != 1 {
					t.Fatalf("wrong error/log count: status=%d logs=%d body=%s", w.Code, len(logs), w.Body.String())
				}
				l := logs[0]
				if l.Protocol != protocol || l.Status != want || l.ErrorKind != "invalid_request" || l.RequestID == "" || l.Attempts != 0 || len(l.AttemptDetails) != 0 || l.Usage.Known || l.UsageIncomplete || l.FirstTokenMS != nil {
					t.Fatalf("rejected inference accounting incorrect: %+v", l)
				}
				if usage := s.Store.Keys()[0].BudgetUsage; usage != (store.BudgetUsage{}) {
					t.Fatalf("rejected request consumed/invalidated budget: %+v", usage)
				}
				after := s.Store.Account("a")
				if before.Status != after.Status || before.Failures != after.Failures || before.CooldownUntil != after.CooldownUntil || s.Store.PoolSnapshot()["a"].InFlight != 0 {
					t.Fatal("input error penalized account or leaked lease")
				}
			})
		}
	}
}

func TestTimingFailureCategories(t *testing.T) {
	for _, tc := range []struct {
		err  error
		kind string
	}{
		{context.Canceled, "canceled"},
		{context.DeadlineExceeded, "timeout"},
		{provider.NewTransportError("Trae", errors.New("failure")), "connection_error"},
		{&provider.Error{Status: 429}, "rate_limit"},
		{&provider.Error{Status: 401, AccountFault: true}, "credentials"},
		{&provider.Error{Status: 500}, "upstream_error"},
	} {
		if got := failureKind(context.Background(), tc.err); got != tc.kind {
			t.Errorf("%T: got %s want %s", tc.err, got, tc.kind)
		}
	}
}
