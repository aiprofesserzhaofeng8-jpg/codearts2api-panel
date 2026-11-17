package providerhttp

import (
	"codearts2api/internal/provider"
	"context"
	"strings"
	"testing"
)

func TestNativeStreamRequiresTerminalAndPreservesFields(t *testing.T) {
	for _, wire := range []string{
		"data: {\"type\":\"message_start\",\"message\":{}}\n\n",
		"data: {\"type\":\"message_start\",\"message\":{}}\n\ndata: {\"type\":\"message_stop\"}\n\n",
		"data: {\"type\":\"message_start\",\"message\":{}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n",
	} {
		if err := NativeSSE(context.Background(), strings.NewReader(wire), provider.MessagesProtocol, "test", func([]byte) error { return nil }); err == nil {
			t.Fatal("accepted incomplete stream")
		}
	}
}

func TestNativeCodexEnvelopeKeepsLegacyControls(t *testing.T) {
	body, err := NativeBody(provider.Request{Model: "model", Protocol: provider.ResponsesProtocol, Body: []byte(`{"input":"hello","max_output_tokens":42,"opaque":{"n":9007199254740993}}`)}, true)
	if err != nil || strings.Contains(string(body), "max_output_tokens") || !strings.Contains(string(body), `"type":"input_text"`) || !strings.Contains(string(body), `"n":9007199254740993`) {
		t.Fatal(string(body), err)
	}
}
