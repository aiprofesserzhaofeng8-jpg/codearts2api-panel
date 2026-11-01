package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"codearts2api/internal/gateway"
	"codearts2api/internal/provider"
)

var _ gateway.StreamTransport = StreamSession{}

type streamFunc func(context.Context, json.RawMessage, provider.Request, func([]byte) error) error

func (f streamFunc) Stream(ctx context.Context, credentials json.RawMessage, input provider.Request, next func([]byte) error) error {
	return f(ctx, credentials, input, next)
}

func TestStreamSessionPreservesCredentialsBodyAndRoutingIDs(t *testing.T) {
	for _, model := range []string{"workbuddy/cn/version-4.1", "workbuddy/global/version-4.1", "trae/cn/version-4.1"} {
		t.Run(model, func(t *testing.T) {
			credentials := json.RawMessage(` {"accessToken":"fixture","future":9007199254740993} `)
			body := []byte(` {"stream":false,"tools":[{"type":"function","function":{"name":"read"}}],"future":9007199254740995} `)
			chunk := []byte(`{"choices":[{"delta":{"refusal":"declined"}}]}`)
			stopped := errors.New("consumer stopped")
			calls := 0
			adapter := streamFunc(func(ctx context.Context, got json.RawMessage, input provider.Request, next func([]byte) error) error {
				calls++
				if ctx != t.Context() || &got[0] != &credentials[0] || !bytes.Equal(got, credentials) || !bytes.Equal(input.Body, body) {
					t.Fatal("context, credential generation or raw body changed")
				}
				if input.Model != "version-4.1" || input.SessionID != "sticky-session" || input.RequestID != "request-correlation" {
					t.Fatal("upstream model or request identifiers changed")
				}
				return next(chunk)
			})
			session := BindStream(adapter, credentials, "sticky-session", "request-correlation")
			err := session.Stream(t.Context(), model, body, func(got []byte) error {
				if &got[0] != &chunk[0] {
					t.Fatal("stream was copied or transformed")
				}
				return stopped
			})
			if !errors.Is(err, stopped) || calls != 1 {
				t.Fatal("consumer error hidden or another upstream call was made")
			}
		})
	}
}

func TestStreamSessionReturnsOriginalPlatformAndCancellationErrors(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, &provider.Error{Status: 500, Code: "fixture", Message: "upstream failed"}} {
		session := BindStream(streamFunc(func(_ context.Context, _ json.RawMessage, _ provider.Request, _ func([]byte) error) error {
			return failure
		}), nil, "", "")
		if err := session.Stream(t.Context(), "trae/cn/model", nil, func([]byte) error { t.Fatal("unexpected output"); return nil }); err != failure {
			t.Fatal("platform or cancellation error was replaced")
		}
	}
}
