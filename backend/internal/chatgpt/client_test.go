package chatgpt

import (
	"context"
	"encoding/base64"
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

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func token(account, user string) string {
	return "header." + base64.RawURLEncoding.EncodeToString(raw(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "sub": user, "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account, "chatgpt_user_id": user}})) + ".fixture"
}
func event(kind string, v any) string { return "event: " + kind + "\ndata: " + string(raw(v)) + "\n\n" }
func completeStream() string {
	return event("response.created", map[string]any{"type": "response.created", "response": map[string]string{"id": "resp_fixture"}}) + event("response.output_text.delta", map[string]string{"type": "response.output_text.delta", "delta": "CHATGPT_COMPLETE"}) + event("response.completed", map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "error": nil, "usage": map[string]any{"input_tokens": 12, "output_tokens": 4, "input_tokens_details": map[string]int{"cached_tokens": 3}}}})
}
func TestOfficialAuthJSONAndRefreshPreserveIdentity(t *testing.T) {
	c := New()
	b, id, e := c.Normalize(raw(map[string]any{"tokens": map[string]string{"access_token": token("account", "user"), "refresh_token": "refresh-fixture"}}))
	if e != nil || !id.HasRefresh || id.Scope != "global" {
		t.Fatal(e, id)
	}
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] != "refresh_token" || body["refresh_token"] != "refresh-fixture" {
			t.Error("bad refresh")
		}
		fmt.Fprint(w, `{"access_token":"rotated-opaque-fixture","refresh_token":"rotated-refresh-fixture","expires_in":3600}`)
	}))
	defer up.Close()
	c.Token = up.URL
	next, e := c.Refresh(context.Background(), b)
	if e != nil {
		t.Fatal(e)
	}
	_, newID, e := c.Normalize(next)
	if e != nil || newID.ID != id.ID || calls != 1 || !strings.Contains(string(next), "rotated-refresh-fixture") {
		t.Fatal("rotation lost or identity changed", e, id, newID)
	}
	for _, bad := range []string{`{"api_key":"sk-fixture","access_token":"mixed"}`, `{"access_token":"opaque"}`, `{"api_key":"sk-fixture","scope":"cn"}`} {
		if _, _, e = c.Normalize([]byte(bad)); e == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	_, static, e := c.Normalize([]byte(`{"OPENAI_API_KEY":"sk-fixture"}`))
	if e != nil || !static.NonExpiring || static.ExpiresAt != 0 {
		t.Fatal(e)
	}
}
func TestResponsesRequestToolHistoryAndPrecision(t *testing.T) {
	r := provider.Request{Model: "actual-model", Body: []byte(`{"messages":[{"role":"system","content":"SYSTEM_CONTEXT"},{"role":"user","content":[{"type":"text","text":"QUESTION_CONTEXT"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call_fixture","type":"function","function":{"name":"lookup","arguments":"{\"id\":9007199254740993}"}}]},{"role":"tool","tool_call_id":"call_fixture","content":"RESULT"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"id":{"const":9007199254740993}}}}}],"max_tokens":64}`)}
	v, e := responsesBody(r, false)
	if e != nil {
		t.Fatal(e)
	}
	b := string(raw(v))
	for _, want := range []string{"SYSTEM_CONTEXT", "QUESTION_CONTEXT", "input_image", "function_call_output", "9007199254740993", "max_output_tokens"} {
		if !strings.Contains(b, want) {
			t.Fatal("lost request field", want, b)
		}
	}
	r.Body = []byte(`{"messages":[{"role":"user","content":"hi"}],"temperature":0}`)
	if _, e = responsesBody(r, true); e == nil {
		t.Fatal("silently ignored unsupported parameter")
	}
}
func TestResponsesCompletionToolsAndFailures(t *testing.T) {
	var chunks [][]byte
	collect := func(b []byte) error { chunks = append(chunks, append([]byte(nil), b...)); return nil }
	if e := responsesStream(context.Background(), strings.NewReader(completeStream()), "chatgpt/global/model", collect); e != nil {
		t.Fatal(e)
	}
	b, e := protocol.AggregateSSE(chunks)
	if e != nil || !strings.Contains(string(b), "CHATGPT_COMPLETE") || !strings.Contains(string(b), `"cached_tokens":3`) {
		t.Fatal(e, string(b))
	}
	stream := event("response.output_item.added", map[string]any{"item": map[string]string{"type": "function_call", "id": "item1", "call_id": "call1", "name": "lookup"}}) + event("response.function_call_arguments.delta", map[string]string{"item_id": "item1", "delta": "{\"id\":9007199254740993}"}) + event("response.function_call_arguments.done", map[string]string{"item_id": "item1", "arguments": "{\"id\":9007199254740993}"}) + event("response.output_item.done", map[string]any{"item": map[string]string{"type": "function_call", "id": "item1", "call_id": "call1", "name": "lookup", "arguments": "{\"id\":9007199254740993}"}}) + event("response.completed", map[string]any{"response": map[string]string{"status": "completed"}})
	chunks = nil
	if e = responsesStream(context.Background(), strings.NewReader(stream), "m", collect); e != nil {
		t.Fatal(e)
	}
	b, e = protocol.AggregateSSE(chunks)
	if e != nil || !strings.Contains(string(b), "9007199254740993") || !strings.Contains(string(b), `"finish_reason":"tool_calls"`) {
		t.Fatal(e, string(b))
	}
	for _, bad := range []string{"", event("response.output_text.delta", map[string]string{"delta": "partial"}), event("response.failed", map[string]string{"private": "secret"}), event("response.completed", map[string]any{"response": map[string]string{"status": "in_progress"}}), event("response.completed", map[string]any{"response": map[string]any{"status": "completed", "output": []any{}}})} {
		if e = responsesStream(context.Background(), strings.NewReader(bad), "m", func([]byte) error { return nil }); e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatal("false success or secret leak", e)
		}
	}
}
