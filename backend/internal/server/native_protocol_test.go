package server

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNativeProtocolRetainsRequestAndResponseFields(t *testing.T) {
	for _, p := range []string{provider.Claude, provider.ChatGPT} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", p, stream), func(t *testing.T) {
				calls := 0
				s, key, g, a := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					var body map[string]json.RawMessage
					_ = json.NewDecoder(r.Body).Decode(&body)
					if string(body["native_extension"]) != `{"n":9007199254740993}` {
						t.Errorf("request lost opaque native extension: %s", body["native_extension"])
					}
					if p == provider.Claude && string(body["system"]) != `[{"type":"text","text":"SYS","cache_control":{"type":"ephemeral"}}]` {
						t.Errorf("system metadata lost: %s", body["system"])
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if p == provider.Claude {
						subscriptionEvent(w, "message_start", map[string]any{"message": map[string]any{"id": "native-id", "role": "assistant", "native_extension": json.RawMessage(`{"n":9007199254740993}`), "usage": map[string]int{"input_tokens": 3, "output_tokens": 0}}})
						subscriptionEvent(w, "content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
						subscriptionEvent(w, "content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": "OK"}})
						subscriptionEvent(w, "content_block_stop", map[string]int{"index": 0})
						subscriptionEvent(w, "message_delta", map[string]any{"delta": map[string]string{"stop_reason": "end_turn"}, "usage": map[string]int{"output_tokens": 1}})
						subscriptionEvent(w, "message_stop", map[string]string{})
					} else {
						subscriptionEvent(w, "response.created", map[string]any{"response": map[string]string{"id": "native-id", "status": "in_progress"}})
						subscriptionEvent(w, "response.output_text.delta", map[string]string{"delta": "OK"})
						subscriptionEvent(w, "response.completed", map[string]any{"response": map[string]any{"id": "native-id", "object": "response", "status": "completed", "model": "actual-model", "output": []any{}, "native_extension": json.RawMessage(`{"n":9007199254740993}`), "usage": map[string]int{"input_tokens": 3, "output_tokens": 1}}})
					}
				})
				var adapter provider.Adapter = g
				if p == provider.Claude {
					adapter = a
				}
				addSubscription(t, s, adapter, true, time.Now().Add(time.Hour))
				body := fmt.Sprintf(`{"model":%q,"stream":%v,"native_extension":{"n":9007199254740993},"input":"test"}`, p+"/global/actual-model", stream)
				path := "/v1/responses"
				if p == provider.Claude {
					path = "/v1/messages"
					body = fmt.Sprintf(`{"model":%q,"stream":%v,"max_tokens":64,"native_extension":{"n":9007199254740993},"system":[{"type":"text","text":"SYS","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"test"}]}`, p+"/global/actual-model", stream)
				}
				result := responseRequest(s, key, path, body)
				if result.Code != 200 || calls != 1 || !strings.Contains(result.Body.String(), `"native_extension":{"n":9007199254740993}`) || !strings.Contains(result.Body.String(), "native-id") {
					t.Fatalf("%d %s calls=%d", result.Code, result.Body.String(), calls)
				}
				log := s.Store.Logs(1)[0]
				if !log.Usage.Known || log.Usage.InputTokens != 3 || log.Usage.OutputTokens != 1 {
					t.Fatalf("usage %+v", log.Usage)
				}
			})
		}
	}
}

func TestNativeMessagesDoesNotReplayAfterOutput(t *testing.T) {
	calls := 0
	s, key, _, a := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		subscriptionEvent(w, "message_start", map[string]any{"message": map[string]any{"id": "partial", "role": "assistant", "usage": map[string]int{"input_tokens": 3, "output_tokens": 0}}})
		subscriptionEvent(w, "content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		subscriptionEvent(w, "content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": "partial output"}})
	})
	addSubscription(t, s, a, true, time.Now().Add(time.Hour))
	s.Store.UpdateSettings(func(settings *storage.Settings) { settings.MaxRetries = 3 })
	result := responseRequest(s, key, "/v1/messages", `{"model":"claude/global/actual-model","stream":true,"max_tokens":64,"messages":[{"role":"user","content":"test"}]}`)
	if calls != 1 || !strings.Contains(result.Body.String(), "partial output") || !strings.Contains(result.Body.String(), "event: error") || strings.Contains(result.Body.String(), "event: message_stop") {
		t.Fatal(calls, result.Body.String())
	}
	if s.Store.PoolSnapshot()["claude-fixture"].InFlight != 0 {
		t.Fatal("lease leaked")
	}
	if s.Store.Logs(1)[0].Usage.Known {
		t.Fatal("partial input usage became a full receipt")
	}
}
