package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

type fixtureSession struct {
	chat     func(context.Context, string, []byte) ([]byte, error)
	stream   func(context.Context, string, []byte, func([]byte) error) error
	fallback error
}

func (s fixtureSession) Chat(ctx context.Context, model string, body []byte) ([]byte, error) {
	return s.chat(ctx, model, body)
}
func (s fixtureSession) Stream(ctx context.Context, model string, body []byte, next func([]byte) error) error {
	return s.stream(ctx, model, body, next)
}
func (s fixtureSession) NeedsStreamFallback(err error) bool { return errors.Is(err, s.fallback) }

func TestCompletePreservesBytesAndObservationBoundary(t *testing.T) {
	body := []byte(`{"stream":false,"messages":[{"role":"user","content":"keep"}],"extension":{"id":9007199254740993}}`)
	reply := []byte(" {\"choices\":[{\"message\":{\"content\":null,\"refusal\":\"declined\"},\"finish_reason\":\"stop\"}],\"future\":9007199254740993}\n")
	calls, usage, observed := 0, 0, 0
	session := fixtureSession{chat: func(_ context.Context, model string, got []byte) ([]byte, error) {
		calls++
		if model != "physical-model" || !bytes.Equal(got, body) {
			t.Fatal("request was rewritten by executor")
		}
		return reply, nil
	}}
	e := &Completions{}
	got, err := e.Complete(t.Context(), session, provider.Model{ID: "physical-model"}, body, Observer{
		Observe: func() { observed++ }, Usage: func(_ context.Context, raw []byte) {
			usage++
			if !bytes.Equal(raw, reply) {
				t.Fatal("usage capture did not receive original reply")
			}
		},
	})
	if err != nil || !bytes.Equal(got, reply) || calls != 1 || usage != 1 || observed != 0 {
		t.Fatalf("reply/call/observation behavior changed: %v %d %d %d", err, calls, usage, observed)
	}
}

func TestStreamFallbackCopiesBorrowedChunksAndPreservesExtensions(t *testing.T) {
	rejected := errors.New("empty rejection")
	calls, streams, marked, usage, observed := 0, 0, 0, 0, 0
	buffer := make([]byte, 512)
	session := fixtureSession{
		fallback: rejected,
		chat:     func(context.Context, string, []byte) ([]byte, error) { calls++; return nil, rejected },
		stream: func(_ context.Context, model string, body []byte, next func([]byte) error) error {
			streams++
			var req map[string]json.RawMessage
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatal(err)
			}
			if model != "physical-model" || string(req["stream"]) != "true" || string(req["extension"]) != "9007199254740993" || !strings.Contains(string(req["stream_options"]), `"future":9007199254740995`) || !strings.Contains(string(req["stream_options"]), `"include_usage":true`) {
				t.Fatal("fallback changed model, extensions or usage options")
			}
			for _, raw := range []string{
				`{"id":"same","choices":[{"index":0,"delta":{"content":"A"}}]}`,
				`{"id":"same","choices":[{"index":0,"delta":{"content":"B"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
			} {
				n := copy(buffer, raw)
				if err := next(buffer[:n]); err != nil {
					return err
				}
			}
			return nil
		},
	}
	e := &Completions{MarkStreamOnly: func(model string) {
		marked++
		if model != "physical-model" {
			t.Fatal("marked a public model")
		}
	}}
	got, err := e.Complete(t.Context(), session, provider.Model{ID: "physical-model"}, []byte(`{"stream":false,"extension":9007199254740993,"stream_options":{"future":9007199254740995}}`), Observer{
		Observe: func() { observed++ }, Usage: func(context.Context, []byte) { usage++ },
	})
	if err != nil || !strings.Contains(string(got), `"content":"AB"`) || calls != 1 || streams != 1 || marked != 1 || usage != 2 || observed != 2 {
		t.Fatalf("fallback/copy/accounting behavior changed: %v %s %d/%d/%d/%d/%d", err, got, calls, streams, marked, usage, observed)
	}
}

func TestFallbackFailureNeverRetriesOrLeaksPartialReply(t *testing.T) {
	rejected := errors.New("empty rejection")
	for _, streamOnly := range []bool{false, true} {
		for _, failure := range []error{rejected, context.Canceled, context.DeadlineExceeded} {
			calls, streams, usage := 0, 0, 0
			session := fixtureSession{fallback: rejected,
				chat: func(context.Context, string, []byte) ([]byte, error) { calls++; return nil, rejected },
				stream: func(_ context.Context, _ string, _ []byte, next func([]byte) error) error {
					streams++
					if err := next([]byte(`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`)); err != nil {
						return err
					}
					return failure
				},
			}
			e := &Completions{MarkStreamOnly: func(string) {}}
			got, err := e.Complete(t.Context(), session, provider.Model{ID: "physical", StreamOnly: streamOnly}, []byte(`{"stream":false}`), Observer{
				Observe: func() {}, Usage: func(context.Context, []byte) { usage++ },
			})
			wantCalls := 1
			if streamOnly {
				wantCalls = 0
			}
			if !errors.Is(err, failure) || len(got) != 0 || calls != wantCalls || streams != 1 || usage != 1 {
				t.Fatal("partial result escaped, cancellation changed, or another attempt was sent")
			}
		}
	}
}

func TestStreamDeliversImmediatelyAndStopsOnConsumerError(t *testing.T) {
	stopped := errors.New("consumer stopped")
	order := ""
	session := fixtureSession{stream: func(_ context.Context, _ string, _ []byte, next func([]byte) error) error {
		order += "upstream;"
		err := next([]byte(`{"choices":[{"delta":{"content":"first"}}]}`))
		if !errors.Is(err, stopped) || order != "upstream;observe;usage;consumer;" {
			t.Fatal("callback was buffered or its error was hidden")
		}
		return err
	}}
	err := (&Completions{}).Stream(t.Context(), session, "physical", nil, Observer{
		Observe: func() { order += "observe;" }, Usage: func(context.Context, []byte) { order += "usage;" },
	}, func([]byte) error { order += "consumer;"; return stopped })
	if !errors.Is(err, stopped) {
		t.Fatal("consumer error changed")
	}
}

func TestCompleteStreamKeepsBodyAndCopiesBorrowedToolChunks(t *testing.T) {
	body := []byte(` {"stream":false,"future":9007199254740993,"stream_options":{"future":true}} `)
	buffer := make([]byte, 512)
	calls, observed, usage := 0, 0, 0
	session := fixtureSession{stream: func(_ context.Context, model string, got []byte, next func([]byte) error) error {
		calls++
		if model != "workbuddy/cn/physical" || !bytes.Equal(body, got) {
			t.Fatal("stream-only aggregation rewrote request fields")
		}
		for _, raw := range []string{
			`{"id":"same","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"tool","type":"function","function":{"name":"read","arguments":"{\"id\":"}}]}}]}`,
			`{"id":"same","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"9007199254740993}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
		} {
			n := copy(buffer, raw)
			if err := next(buffer[:n]); err != nil {
				return err
			}
		}
		return nil
	}}
	got, err := (&Completions{}).CompleteStream(t.Context(), session, "workbuddy/cn/physical", body, Observer{
		Observe: func() { observed++ }, Usage: func(context.Context, []byte) { usage++ },
	})
	if err != nil || calls != 1 || observed != 2 || usage != 2 || !strings.Contains(string(got), `\"id\":9007199254740993`) || !strings.Contains(string(got), `"finish_reason":"tool_calls"`) {
		t.Fatalf("tool aggregation/accounting changed: %v %s", err, got)
	}
}

func TestCompleteStreamFailureCountsPartialUsageWithoutReplay(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, &provider.Error{Status: 400, ContentBlocked: true, Message: "blocked"}} {
		calls, observed, usage := 0, 0, 0
		session := fixtureSession{stream: func(_ context.Context, _ string, _ []byte, next func([]byte) error) error {
			calls++
			if err := next([]byte(`{"choices":[{"delta":{"content":"partial"}}],"usage":{"prompt_tokens":3}}`)); err != nil {
				return err
			}
			return failure
		}}
		got, err := (&Completions{}).CompleteStream(t.Context(), session, "trae/cn/physical", nil, Observer{
			Observe: func() { observed++ }, Usage: func(context.Context, []byte) { usage++ },
		})
		if err != failure || got != nil || calls != 1 || usage != 1 || observed != 1 {
			t.Fatal("partial output replayed/returned or usage/observation lost")
		}
	}
}

func TestCompleteStreamRetains32MiBLimitAndCountsRejectedChunk(t *testing.T) {
	chunk := make([]byte, 1<<20)
	observed, usage, calls := 0, 0, 0
	session := fixtureSession{stream: func(_ context.Context, _ string, _ []byte, next func([]byte) error) error {
		calls++
		for i := range 33 {
			if err := next(chunk); err != nil {
				if i != 32 {
					t.Fatal("aggregate stopped below its existing limit")
				}
				return err
			}
		}
		t.Fatal("aggregate did not stop at its existing limit")
		return nil
	}}
	got, err := (&Completions{}).CompleteStream(t.Context(), session, "workbuddy/cn/physical", nil, Observer{
		Observe: func() { observed++ }, Usage: func(context.Context, []byte) { usage++ },
	})
	if err == nil || err.Error() != "上游响应超过 32 MiB" || got != nil || calls != 1 || observed != 33 || usage != 33 {
		t.Fatal("size failure changed, replayed or lost accounting", err)
	}
}
