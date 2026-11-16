package opencode

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func nativeEvent(kind, body string) string { return "event: " + kind + "\ndata: " + body + "\n\n" }
func chatOK() string {
	return "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
}
func TestNativeProtocolsAndStableSession(t *testing.T) {
	responsesOK := nativeEvent("response.output_text.delta", `{"delta":"OK"}`) + nativeEvent("response.completed", `{"response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":1}}}`)
	messagesOK := nativeEvent("message_start", `{"message":{"id":"test","role":"assistant","usage":{"input_tokens":2,"output_tokens":0}}}`) + nativeEvent("content_block_start", `{"index":0,"content_block":{"type":"text","text":""}}`) + nativeEvent("content_block_delta", `{"index":0,"delta":{"type":"text_delta","text":"OK"}}`) + nativeEvent("content_block_stop", `{"index":0}`) + nativeEvent("message_delta", `{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`) + nativeEvent("message_stop", `{}`)
	for _, tc := range []struct{ scope, model, path, wire, field string }{
		{"zen", "deepseek-v4.1-flash", "/openai/v1/chat/completions", chatOK(), "messages"},
		{"go", "gpt-test", "/go/openai/v1/responses", responsesOK, "input"},
		{"go", "qwen3.8-max", "/go/anthropic/v1/messages", messagesOK, "messages"},
	} {
		t.Run(tc.scope+tc.model, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != tc.path || r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("x-opencode-session") != "conversation-1" || r.Header.Get("User-Agent") != "codearts2api-panel/opencode" {
					t.Error("wrong route or headers")
				}
				var body map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&body) != nil || string(body["model"]) != `"`+tc.model+`"` || string(body["stream"]) != "true" || len(body[tc.field]) == 0 {
					t.Error("invalid native body")
				}
				encoded, _ := json.Marshal(body)
				if !strings.Contains(string(encoded), "aGVsbG8=") || !strings.Contains(string(encoded), "inspect_file") {
					t.Error("lost image or tool definition")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, tc.wire)
			}))
			defer s.Close()
			c := New()
			c.Origin = s.URL
			for i := 0; i < 2; i++ {
				var out strings.Builder
				err := c.Stream(context.Background(), rawKey(tc.scope), provider.Request{Model: tc.model, SessionID: "conversation-1", Body: []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}],"tools":[{"type":"function","function":{"name":"inspect_file","parameters":{"type":"object","properties":{}}}}]}`)}, func(b []byte) error { out.Write(b); return nil })
				if err != nil || !strings.Contains(out.String(), "OK") {
					t.Fatal(err, out.String())
				}
			}
			if calls != 2 {
				t.Fatal("extra catalog/retry request", calls)
			}
		})
	}
}
func TestVerifyRequiresAuthenticatedInference(t *testing.T) {
	for _, status := range []int{200, 401, 403, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/v1/models" {
					io.WriteString(w, `{"data":[{"id":"minimax-m3.1-flash","type":"chat","api":{"chat-completions":{}}}]}`)
					return
				}
				if r.URL.Path != "/openai/v1/chat/completions" {
					t.Error(r.URL.Path)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(status)
				if status == 200 {
					io.WriteString(w, chatOK())
				}
			}))
			defer s.Close()
			c := New()
			c.Origin = s.URL
			_, err := c.Verify(context.Background(), rawKey("zen"))
			if (err == nil) != (status == 200) || calls != 2 {
				t.Fatal(err, calls)
			}
		})
	}
}
func TestStreamFailureAndCancellation(t *testing.T) {
	for _, wire := range []string{"", "data: [DONE]\n\n", "data: {\"error\":{}}\n\n", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, wire)
		}))
		c := New()
		c.Origin = s.URL
		if err := c.Stream(context.Background(), rawKey("go"), provider.Request{Model: "deepseek-v4.1-flash", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil }); err == nil {
			t.Error("accepted incomplete stream")
		}
		s.Close()
	}
	c := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Stream(ctx, rawKey("go"), provider.Request{Model: "deepseek-v4.1-flash", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
