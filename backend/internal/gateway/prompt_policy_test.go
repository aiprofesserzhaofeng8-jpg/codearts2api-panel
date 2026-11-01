package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"codearts2api/internal/provider"
	"codearts2api/internal/requestpolicy"
)

func TestPromptRecoveryPreservesBodyAndOrderedAttemptBoundary(t *testing.T) {
	body := []byte(` {"messages":[{"role":"system","content":"CLIENT"},{"role":"user","content":"cc_version=42; keep 11128"},{"role":"assistant","reasoning_content":"keep","tool_calls":[{"id":"tool-1","function":{"arguments":"{\"n\":9007199254740993}"}}]}],"seed":9007199254740993} `)
	original := append([]byte(nil), body...)
	blocked := fmt.Errorf("wrapped: %w", &provider.Error{Status: 400, ContentBlocked: true})
	var order []string
	calls := 0
	recovery := PromptRecovery{
		BeforeRetry: func(err error) error {
			if err != blocked {
				t.Fatal("changed blocked error")
			}
			order = append(order, "finish-and-budget")
			return nil
		},
		StartRetry: func() { order = append(order, "start") },
	}
	err := RunPromptPolicy(t.Context(), body, PromptPolicy{Mode: "passthrough", Fallback: true}, recovery, func(prepared []byte, _ func()) error {
		calls++
		order = append(order, fmt.Sprintf("run-%d", calls))
		if calls == 1 {
			if &prepared[0] != &body[0] {
				t.Fatal("no-op passthrough copied the request")
			}
		} else if !strings.Contains(string(prepared), requestpolicy.FallbackSystemPrompt) || strings.Contains(string(prepared), "CLIENT") || !strings.Contains(string(prepared), `9007199254740993`) || !strings.Contains(string(prepared), `cc_version=42; keep 11128`) || !strings.Contains(string(prepared), `"reasoning_content":"keep"`) {
			t.Fatal("fallback lost business data or failed to replace the system prompt")
		}
		return blocked
	})
	if err != blocked || calls != 2 || !reflect.DeepEqual(order, []string{"run-1", "finish-and-budget", "start", "run-2"}) || !bytes.Equal(body, original) {
		t.Fatalf("changed retry limit, order, error or original input: %v %d %v", err, calls, order)
	}
}

func TestPromptRecoveryDoesNotReplayObservedOrIneligibleAttempts(t *testing.T) {
	blocked := &provider.Error{Status: 400, ContentBlocked: true}
	permission := &provider.Error{Status: 403, Message: "not authorized"}
	for _, tc := range []struct {
		name, mode                 string
		fallback, observed, cancel bool
		failure                    error
	}{
		{"observed-empty-or-buffered-frame", "passthrough", true, true, false, blocked},
		{"disabled", "passthrough", false, false, false, blocked},
		{"custom", "custom", true, false, false, blocked},
		{"canceled", "passthrough", true, false, true, blocked},
		{"permission", "passthrough", true, false, false, permission},
		{"ordinary-error", "passthrough", true, false, false, errors.New("transport")},
		{"success", "passthrough", true, false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			hooks := PromptRecovery{BeforeRetry: func(error) error { t.Fatal("unexpected recovery"); return nil }, StartRetry: func() { t.Fatal("unexpected attempt") }}
			err := RunPromptPolicy(ctx, []byte(`{"messages":[{"role":"user","content":"hello"}]}`), PromptPolicy{Mode: tc.mode, Fallback: tc.fallback}, hooks, func(_ []byte, observe func()) error {
				calls++
				if tc.observed {
					observe()
				}
				if tc.cancel {
					cancel()
				}
				return tc.failure
			})
			if err != tc.failure || calls != 1 {
				t.Fatal("original result changed or request replayed")
			}
		})
	}
}

func TestPromptRecoveryBudgetAndPreparationFailuresStopBeforeAttempt(t *testing.T) {
	blocked := &provider.Error{Status: 400, ContentBlocked: true}
	denied := errors.New("existing budget denied")
	for _, tc := range []struct {
		name, body, mode string
		budget           error
		calls, before    int
		want             error
	}{
		{"budget", `{"messages":[]}`, "passthrough", denied, 1, 1, denied},
		{"initial-preparation", `{`, "custom", nil, 0, 0, nil},
		{"replacement-preparation", `{`, "passthrough", nil, 1, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, before := 0, 0
			err := RunPromptPolicy(t.Context(), []byte(tc.body), PromptPolicy{Mode: tc.mode, Fallback: true}, PromptRecovery{
				BeforeRetry: func(got error) error {
					before++
					if got != blocked {
						t.Fatal("changed original failure")
					}
					return tc.budget
				},
				StartRetry: func() { t.Fatal("started forbidden or unprepared attempt") },
			}, func([]byte, func()) error { calls++; return blocked })
			if calls != tc.calls || before != tc.before {
				t.Fatalf("wrong attempt/budget boundary: %d %d", calls, before)
			}
			if tc.want != nil {
				if err != tc.want {
					t.Fatal("budget failure replaced")
				}
				return
			}
			var invalid *provider.Error
			if !errors.As(err, &invalid) || invalid.Status != 400 || invalid.AccountFault {
				t.Fatal("preparation error changed classification")
			}
		})
	}
}
