package cnapi

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtocolConfigAndNativeEndpoint(t *testing.T) {
	for _, id := range []string{"kimi", "deepseek", "minimax", "zhipu"} {
		t.Run(id, func(t *testing.T) {
			c := New(id, id, "https://fixture.invalid/v1", "", nil)
			raw, _, err := c.Normalize([]byte(`{"api_key":"fixture"}`))
			if err != nil {
				t.Fatal(err)
			}
			if p, err := c.ResolveProtocol(raw, provider.Model{}, provider.MessagesProtocol); err != nil || p != provider.ChatProtocol {
				t.Fatal(p, err)
			}
			raw, err = c.SetProtocol(raw, provider.AdaptiveProtocol)
			if err != nil {
				t.Fatal(err)
			}
			if p, err := c.ResolveProtocol(raw, provider.Model{}, provider.ResponsesProtocol); err != nil || id == "zhipu" && p != provider.ChatProtocol || id != "zhipu" && p != provider.ResponsesProtocol {
				t.Fatal(p, err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/anthropic/v1/messages" || r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("anthropic-version") != "2023-06-01" {
					t.Error(r.URL.Path, r.Header)
				}
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				if string(body["opaque"]) != `{"n":9007199254740993}` {
					t.Error(string(body["opaque"]))
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			}))
			defer server.Close()
			c.Base = server.URL + "/v1"
			err = c.StreamNative(context.Background(), raw, provider.Request{Model: "model", Protocol: provider.MessagesProtocol, Body: []byte(`{"messages":[{"role":"user","content":"test"}],"opaque":{"n":9007199254740993}}`)}, func([]byte) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
