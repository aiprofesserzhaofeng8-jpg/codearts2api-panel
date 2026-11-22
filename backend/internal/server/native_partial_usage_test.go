package server

import (
	"context"
	"testing"
)

func TestNativePartialReceiptSurvivesRetry(t *testing.T) {
	ctx := withUsageCapture(context.Background(), "messages")
	resetUsage(ctx)
	var n nativeUsage
	start := []byte(`{"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":20}}}`)
	n.capture(ctx, start)
	if u := requestUsage(ctx); u.Known || !u.Partial || u.InputTokens != 120 || u.CachedTokens != 20 || !requestUsageIncomplete(ctx) {
		t.Fatal(u)
	}
	resetUsage(ctx)
	n = nativeUsage{}
	n.capture(ctx, start)
	n.capture(ctx, []byte(`{"type":"message_delta","usage":{"output_tokens":7}}`))
	if u := currentAttemptUsage(ctx); !u.Known || u.Partial || u.InputTokens != 120 || u.OutputTokens != 7 {
		t.Fatal(u)
	}
	if u := requestUsage(ctx); !u.Partial || u.InputTokens != 240 || u.CachedTokens != 40 || u.OutputTokens != 7 || !requestUsageIncomplete(ctx) {
		t.Fatal(u)
	}
}
