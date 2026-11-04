package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/protocol"
	"codearts2api/internal/provider"
)

func raw(v any) json.RawMessage       { b, _ := json.Marshal(v); return b }
func event(kind string, v any) string { return "event: " + kind + "\ndata: " + string(raw(v)) + "\n\n" }
func messageStart() string {
	return event("message_start", map[string]any{"message": map[string]any{"id": "msg_fixture", "role": "assistant", "usage": map[string]int{"input_tokens": 7, "output_tokens": 0, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 2}}})
}
func blockStart(index int, v any) string {
	return event("content_block_start", map[string]any{"index": index, "content_block": v})
}
func blockDelta(index int, v any) string {
	return event("content_block_delta", map[string]any{"index": index, "delta": v})
}
func blockStop(index int) string { return event("content_block_stop", map[string]int{"index": index}) }
func messageEnd(reason string) string {
	return event("message_delta", map[string]any{"delta": map[string]string{"stop_reason": reason}, "usage": map[string]int{"output_tokens": 4}}) + event("message_stop", map[string]any{})
}
func TestRefreshIdentityAndBoundedCatalogPagination(t *testing.T) {
	c := New()
	b, id, e := c.Normalize(raw(Credentials{AccessToken: "old-fixture", RefreshToken: "old-refresh", AccountUUID: "account-fixture", ExpiresAt: time.Now().Add(time.Hour).Unix()}))
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/token":
			fmt.Fprint(w, `{"access_token":"rotated-fixture","refresh_token":"rotated-refresh","expires_in":3600}`)
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer old-fixture" || r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
				t.Error("wrong OAuth headers")
			}
			if r.URL.Query().Get("after_id") == "" {
				fmt.Fprint(w, `{"data":[{"id":"actual-a","display_name":"Actual A"}],"has_more":true,"last_id":"actual-a"}`)
			} else {
				fmt.Fprint(w, `{"data":[{"id":"actual-b","max_input_tokens":100000,"max_tokens":8192}],"has_more":false}`)
			}
		default:
			t.Error("unexpected post-rotation network", r.URL.Path)
		}
	}))
	defer up.Close()
	c.API = up.URL
	c.Token = up.URL + "/token"
	next, e := c.Refresh(context.Background(), b)
	if e != nil {
		t.Fatal(e)
	}
	_, newID, e := c.Normalize(next)
	if e != nil || id.ID != newID.ID || calls != 1 || !strings.Contains(string(next), "rotated-refresh") {
		t.Fatal("rotation not stable", e)
	}
	models, e := c.Models(context.Background(), b)
	if e != nil || len(models) != 2 || models[1].ID != "actual-b" || models[1].MaxOutput != 8192 || calls != 3 {
		t.Fatal(e, models, calls)
	}
	_, key, e := c.Normalize([]byte(`{"api_key":"sk-ant-fixture"}`))
	if e != nil || !key.NonExpiring || key.ExpiresAt != 0 {
		t.Fatal(e)
	}
	for _, bad := range []string{`{"access_token":"opaque"}`, `{"api_key":"sk-ant-fixture","scope":"cn"}`, `{"api_key":"sk-ant-fixture","access_token":"mixed"}`} {
		if _, _, e = c.Normalize([]byte(bad)); e == nil {
			t.Fatal("bad credential accepted")
		}
	}
}
func TestMessagesNativeThinkingToolHistoryAndBigIntegers(t *testing.T) {
	var req protocol.Request
	if e := json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"QUESTION"},{"role":"assistant","content":[{"type":"thinking","thinking":"THOUGHT","signature":"SIGNED"},{"type":"tool_use","id":"call1","name":"lookup","input":{"id":9007199254740993}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"RESULT"}]}],"thinking":{"type":"enabled","budget_tokens":1024},"max_tokens":2048}`), &req); e != nil {
		t.Fatal(e)
	}
	in, e := protocol.ToOpenAINativeMessages(&req, "actual-model")
	if e != nil {
		t.Fatal(e)
	}
	v, e := messagesBody(provider.Request{Model: "actual-model", Body: raw(in)})
	if e != nil {
		t.Fatal(e)
	}
	b := string(raw(v))
	for _, want := range []string{"SIGNED", "THOUGHT", "QUESTION", "RESULT", "9007199254740993", "budget_tokens", "tool_result"} {
		if !strings.Contains(b, want) {
			t.Fatal("lost history", want, b)
		}
	}
	if _, e = protocol.ToOpenAI(&req, "other-platform"); e == nil {
		t.Fatal("native thinking escaped other platform validation")
	}
}
func TestMessagesSignedThinkingToolOutputAndTerminalValidation(t *testing.T) {
	stream := messageStart() + blockStart(0, map[string]string{"type": "thinking", "thinking": ""}) + blockDelta(0, map[string]string{"type": "thinking_delta", "thinking": "THOUGHT"}) + blockDelta(0, map[string]string{"type": "signature_delta", "signature": "SIGNED"}) + blockStop(0) + blockStart(1, map[string]string{"type": "redacted_thinking", "data": "OPAQUE"}) + blockStop(1) + blockStart(2, map[string]any{"type": "tool_use", "id": "call1", "name": "lookup", "input": map[string]any{}}) + blockDelta(2, map[string]string{"type": "input_json_delta", "partial_json": "{\"id\":9007199254740993}"}) + blockStop(2) + messageEnd("tool_use")
	var chunks [][]byte
	collect := func(b []byte) error { chunks = append(chunks, append([]byte(nil), b...)); return nil }
	if e := messagesStream(context.Background(), strings.NewReader(stream), "claude/global/model", collect); e != nil {
		t.Fatal(e)
	}
	b, e := protocol.AggregateSSE(chunks)
	if e != nil {
		t.Fatal(e)
	}
	reply := protocol.FromOpenAI(b, "model")
	payload := string(raw(reply.Payload))
	for _, want := range []string{"SIGNED", "THOUGHT", "OPAQUE", "redacted_thinking", "9007199254740993", `"input_tokens":7`, `"cache_read_input_tokens":3`, `"cache_creation_input_tokens":2`} {
		if !strings.Contains(payload, want) {
			t.Fatal("lost response", want, payload)
		}
	}
	for _, bad := range []string{"", messageStart(), messageStart() + blockStart(0, map[string]string{"type": "text", "text": "partial"}), messageStart() + messageEnd("end_turn"), messageStart() + blockStart(0, map[string]string{"type": "thinking", "thinking": "unsigned"}) + blockStop(0) + messageEnd("end_turn"), event("error", map[string]string{"private": "secret"})} {
		if e = messagesStream(context.Background(), strings.NewReader(bad), "m", func([]byte) error { return nil }); e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatal("false success or secret leak", e)
		}
	}
	// A missing final token count must not become known zero output usage.
	missingUsage := messageStart() + blockStart(0, map[string]string{"type": "text", "text": "hello"}) + blockStop(0) + event("message_delta", map[string]any{"delta": map[string]string{"stop_reason": "end_turn"}, "usage": map[string]any{}}) + event("message_stop", map[string]any{})
	if e = messagesStream(context.Background(), strings.NewReader(missingUsage), "m", func([]byte) error { return nil }); e == nil {
		t.Fatal("missing final usage became zero-cost success")
	}
}
