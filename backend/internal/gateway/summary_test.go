package gateway

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func summaryFrame(text, finish string) []byte {
	return []byte(`{"choices":[{"delta":{"content":"` + text + `"},"finish_reason":"` + finish + `"}],"future":9007199254740993}`)
}

func summaryHooks() SummaryHooks {
	return SummaryHooks{
		Prepare: func(bool) (SummaryAttempt, error) {
			return SummaryAttempt{Model: "physical", Body: []byte("original")}, nil
		},
		Start: func(int, string) {}, Usage: func(context.Context, []byte) {}, Result: func(string, error) {},
	}
}

func TestSummaryPrivateRetryPreservesFramesAndHookOrder(t *testing.T) {
	var events []string
	calls := 0
	buffer := make([]byte, 1024)
	session := fixtureSession{stream: func(_ context.Context, model string, body []byte, next func([]byte) error) error {
		calls++
		if model != "physical" || string(body) != "original" {
			t.Fatal("executor changed authorized model or prepared request")
		}
		if calls == 1 {
			return next(summaryFrame("PRIVATE_PARTIAL", "length"))
		}
		for _, raw := range [][]byte{summaryFrame("A", ""), summaryFrame("B", "stop")} {
			n := copy(buffer, raw)
			if err := next(buffer[:n]); err != nil {
				return err
			}
		}
		clear(buffer)
		return nil
	}}
	hooks := summaryHooks()
	hooks.Prepare = func(retry bool) (SummaryAttempt, error) {
		if retry {
			events = append(events, "prepare2")
		} else {
			events = append(events, "prepare1")
		}
		return SummaryAttempt{Model: "physical", Body: []byte("original")}, nil
	}
	hooks.Start = func(number int, model string) {
		if model != "physical" {
			t.Fatal("accounting received a different model")
		}
		events = append(events, []string{"", "start1", "start2"}[number])
	}
	hooks.Usage = func(context.Context, []byte) { events = append(events, "usage") }
	hooks.Result = func(model string, err error) {
		if model != "physical" || err != nil {
			t.Fatal("unexpected health result")
		}
		events = append(events, "success")
	}
	got, err := RunSummary(t.Context(), session, true, hooks)
	want := [][]byte{summaryFrame("A", ""), summaryFrame("B", "stop")}
	if err != nil || !reflect.DeepEqual(got, want) || calls != 2 {
		t.Fatalf("private buffering, borrowed bytes or unknown fields changed: calls=%d err=%v", calls, err)
	}
	if !reflect.DeepEqual(events, []string{"prepare1", "start1", "usage", "prepare2", "start2", "usage", "usage", "success"}) {
		t.Fatalf("hook order: %v", events)
	}
}

func TestSummaryValidationAndRetryGate(t *testing.T) {
	tool := []byte(`{"choices":[{"delta":{"content":"text","tool_calls":[{"id":"t"}]},"finish_reason":"stop"}]}`)
	for _, tc := range []struct {
		name    string
		frame   []byte
		retry   bool
		calls   int
		usage   int
		results int
		message string
	}{
		{"malformed", []byte(`{"choices":`), true, 1, 0, 1, "无效 JSON"},
		{"empty", summaryFrame("  ", "stop"), true, 1, 1, 0, "没有有效正文"},
		{"missing_finish", summaryFrame("text", ""), true, 1, 1, 0, "未完整结束"},
		{"filtered", summaryFrame("text", "content_filter"), true, 1, 1, 0, "content_filter"},
		{"tools", tool, true, 1, 1, 0, "未完整结束"},
		{"one_limit", summaryFrame("text", "length"), false, 1, 1, 0, "连续 1 次"},
		{"two_limits", summaryFrame("text", "length"), true, 2, 2, 0, "连续 2 次"},
		// Preserve the old length gate even for unexpected tool output. Both
		// attempts remain private and no third attempt is allowed.
		{"tools_at_limit", bytes.Replace(tool, []byte(`"stop"`), []byte(`"length"`), 1), true, 2, 2, 0, "连续 2 次"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, usage, results := 0, 0, 0
			hooks := summaryHooks()
			hooks.Usage = func(context.Context, []byte) { usage++ }
			hooks.Result = func(string, error) { results++ }
			session := fixtureSession{stream: func(_ context.Context, _ string, _ []byte, next func([]byte) error) error {
				calls++
				return next(tc.frame)
			}}
			got, err := RunSummary(t.Context(), session, tc.retry, hooks)
			var terminal *SummaryFailure
			if got != nil || !errors.As(err, &terminal) || !terminal.NonRetryableRequest() || !strings.Contains(err.Error(), tc.message) || calls != tc.calls || usage != tc.usage || results != tc.results {
				t.Fatalf("unexpected result: chunks=%d err=%v calls=%d usage=%d results=%d", len(got), err, calls, usage, results)
			}
		})
	}
}

func TestSummaryPreparationFailureDoesNotStartOrReplay(t *testing.T) {
	for _, failRetry := range []bool{false, true} {
		calls, starts := 0, 0
		rejected := errors.New("budget or permission denied")
		hooks := summaryHooks()
		hooks.Prepare = func(retry bool) (SummaryAttempt, error) {
			if retry == failRetry {
				return SummaryAttempt{}, rejected
			}
			return SummaryAttempt{Model: "physical"}, nil
		}
		hooks.Start = func(int, string) { starts++ }
		session := fixtureSession{stream: func(_ context.Context, _ string, _ []byte, next func([]byte) error) error {
			calls++
			return next(summaryFrame("private", "length"))
		}}
		got, err := RunSummary(t.Context(), session, true, hooks)
		wantCalls := 0
		if failRetry {
			wantCalls = 1
		}
		if got != nil || err != rejected || calls != wantCalls || starts != wantCalls {
			t.Fatalf("failed preparation started an attempt or changed error identity: %v calls=%d starts=%d", err, calls, starts)
		}
	}
}

func TestSummaryCancellationAndTransportFailure(t *testing.T) {
	for _, point := range []string{"before", "retry", "stream"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls, prepared, results := 0, 0, 0
			hooks := summaryHooks()
			hooks.Prepare = func(bool) (SummaryAttempt, error) { prepared++; return SummaryAttempt{}, nil }
			hooks.Result = func(_ string, err error) {
				results++
				if err != context.Canceled {
					t.Fatal("health hook lost original transport error")
				}
			}
			if point == "before" {
				cancel()
			}
			session := fixtureSession{stream: func(_ context.Context, _ string, _ []byte, next func([]byte) error) error {
				calls++
				cancel()
				if point == "stream" {
					return context.Canceled
				}
				return next(summaryFrame("private", "length"))
			}}
			got, err := RunSummary(ctx, session, true, hooks)
			if got != nil || err == nil {
				t.Fatal("canceled summary released private output")
			}
			if point == "stream" {
				var terminal *SummaryFailure
				if !errors.As(err, &terminal) || results != 1 || calls != 1 || prepared != 1 {
					t.Fatalf("stream error changed: %v results=%d calls=%d prepared=%d", err, results, calls, prepared)
				}
			} else if err != context.Canceled || results != 0 || calls > 1 || prepared != calls {
				t.Fatalf("cancellation started another attempt: %v results=%d calls=%d prepared=%d", err, results, calls, prepared)
			}
		})
	}
}

func TestSummaryPrivateBufferLimitAndUsageOrder(t *testing.T) {
	frame := summaryFrame("complete", "stop")
	frame = append(frame, bytes.Repeat([]byte(" "), (1<<20)-len(frame))...)
	for _, count := range []int{8, 9} {
		usage, results := 0, 0
		hooks := summaryHooks()
		hooks.Usage = func(context.Context, []byte) { usage++ }
		hooks.Result = func(string, error) { results++ }
		session := fixtureSession{stream: func(_ context.Context, _ string, _ []byte, next func([]byte) error) error {
			for i := 0; i < count; i++ {
				if err := next(frame); err != nil {
					return err
				}
			}
			return nil
		}}
		got, err := RunSummary(t.Context(), session, true, hooks)
		if usage != count || results != 1 {
			t.Fatalf("rejected chunk usage or health accounting changed: usage=%d results=%d", usage, results)
		}
		if count == 8 && (err != nil || len(got) != 8) {
			t.Fatalf("exact 8 MiB boundary rejected: %v", err)
		}
		if count == 9 && (got != nil || err == nil || !strings.Contains(err.Error(), "超过 8 MiB")) {
			t.Fatalf("oversized summary escaped: chunks=%d err=%v", len(got), err)
		}
	}
}
