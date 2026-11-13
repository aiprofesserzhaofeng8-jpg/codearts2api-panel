package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

func TestToolHistoryNormalizesOnlyEmptyContent(t *testing.T) {
	for _, content := range []string{"", `,"content":null`, `,"content": ""`, `,"content":"visible"`, `,"content":[{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]`} {
		t.Run(content, func(t *testing.T) {
			input := `[{"role":"system","content":"KEEP"},{"role":"user","content":"QUESTION"},{"role":"assistant"` + content + `,"reasoning_content":"EXACT_THOUGHT","tool_calls":[{"id":"call-A","type":"function","function":{"name":"lookup","arguments":"{\"n\":9007199254740993}"}}]},{"role":"tool","tool_call_id":"call-A","content":null},{"role":"assistant","content":null}]`
			var messages []map[string]json.RawMessage
			if err := json.Unmarshal([]byte(input), &messages); err != nil {
				t.Fatal(err)
			}
			before := make([]map[string]json.RawMessage, len(messages))
			_ = json.Unmarshal([]byte(input), &before)
			if !normalizeEmptyToolContent(messages) {
				t.Fatal("tool null not normalized")
			}
			if string(messages[3]["content"]) != `""` {
				t.Fatal("tool content")
			}
			want := before[2]["content"]
			if len(want) == 0 || string(want) == "null" {
				want = json.RawMessage(`""`)
			}
			if string(messages[2]["content"]) != string(want) {
				t.Fatal("non-empty content changed")
			}
			for i := range messages {
				for k, v := range before[i] {
					if k != "content" && !reflect.DeepEqual(messages[i][k], v) {
						t.Fatal("history field changed", i, k)
					}
				}
			}
			if !reflect.DeepEqual(messages[0], before[0]) || !reflect.DeepEqual(messages[1], before[1]) || !reflect.DeepEqual(messages[4], before[4]) {
				t.Fatal("unrelated messages changed")
			}
			if normalizeEmptyToolContent(messages) {
				t.Fatal("normalization not idempotent")
			}
		})
	}
}

func TestSignedToolRoundTripPreservesHistory(t *testing.T) {
	for _, empty := range []string{`null`, `""`} {
		t.Run(empty, func(t *testing.T) {
			calls := 0
			var firstRequestID string
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body struct {
					Messages  []map[string]json.RawMessage
					Tools     json.RawMessage
					RequestID string                               `json:"request_id"`
					SessionID string                               `json:"session_id"`
					RecordID  string                               `json:"chat_record_id"`
					Context   struct{ Text struct{ Text string } } `json:"chat_context"`
				}
				if err := json.Unmarshal(decodedRequest(t, r), &body); err != nil {
					t.Fatal(err)
				}
				if body.SessionID != "stable-session" || body.RecordID != "stable-session" || body.Context.Text.Text != "QUESTION" {
					t.Fatal("session or context changed", body)
				}
				if calls == 1 {
					firstRequestID = body.RequestID
					if len(body.Messages) != 2 {
						t.Fatal("first history changed")
					}
				} else {
					if calls != 2 || len(body.Messages) != 5 || body.RequestID == firstRequestID {
						t.Fatal("history duplicated or request replayed")
					}
					assistant := body.Messages[2]
					if string(assistant["content"]) != `""` || string(assistant["reasoning_content"]) != `"EXACT_THOUGHT"` {
						t.Fatal("assistant dropped or reasoning changed")
					}
					var toolCalls []struct {
						ID       string
						Function struct{ Name, Arguments string }
					}
					if json.Unmarshal(assistant["tool_calls"], &toolCalls) != nil || len(toolCalls) != 2 || toolCalls[0].ID != "call-A" || toolCalls[0].Function.Name != "lookup" || toolCalls[0].Function.Arguments != `{"n":9007199254740993}` || toolCalls[1].ID != "call-B" {
						t.Fatal("tool calls changed", toolCalls)
					}
					if string(body.Messages[3]["tool_call_id"]) != `"call-B"` || string(body.Messages[3]["content"]) != `"RESULT-B"` || string(body.Messages[4]["tool_call_id"]) != `"call-A"` || string(body.Messages[4]["content"]) != `"RESULT-A"` {
						t.Fatal("tool results reordered or detached")
					}
					if !strings.Contains(string(body.Tools), "lookup") {
						t.Fatal("tool declaration dropped")
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				result := `{"choices":[{"index":0,"delta":{"reasoning_content":"EXACT_THOUGHT","tool_calls":[{"index":0,"id":"call-A","type":"function","function":{"name":"lookup","arguments":"{\"n\":9007199254740993}"}},{"index":1,"id":"call-B","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`
				if calls == 2 {
					result = `{"choices":[{"index":0,"delta":{"content":"RESULT-A RESULT-B"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`
				}
				io.WriteString(w, frame(result)+frame("[DONE]")+"event: finish\ndata: {}\n\n")
			})
			base := `{"role":"system","content":"KEEP"},{"role":"user","content":"QUESTION"}`
			tools := `"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]`
			first := provider.Request{Model: "dmodel", SessionID: "stable-session", Body: []byte(`{"messages":[` + base + `],` + tools + `}`)}
			if err := c.Stream(context.Background(), credential(), first, func([]byte) error { return nil }); err != nil {
				t.Fatal(err)
			}
			follow := provider.Request{Model: "dmodel", SessionID: "stable-session", Body: []byte(fmt.Sprintf(`{"messages":[%s,{"role":"assistant","content":%s,"reasoning_content":"EXACT_THOUGHT","tool_calls":[{"id":"call-A","type":"function","function":{"name":"lookup","arguments":"{\"n\":9007199254740993}"}},{"id":"call-B","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-B","content":"RESULT-B"},{"role":"tool","tool_call_id":"call-A","content":"RESULT-A"}],%s}`, base, empty, tools))}
			var received string
			if err := c.Stream(context.Background(), credential(), follow, func(b []byte) error { received += string(b); return nil }); err != nil {
				t.Fatal(err)
			}
			if calls != 2 || !strings.Contains(received, "RESULT-A RESULT-B") {
				t.Fatal("tool result not consumed", calls, received)
			}
		})
	}
}
