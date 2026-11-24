package grok

import (
	"codearts2api/internal/protocol"
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

func event(kind string, obj any) string {
	b, _ := json.Marshal(obj)
	return "event: " + kind + "\ndata: " + string(b) + "\n\n"
}
func completed() string {
	return event("response.completed", map[string]any{"response": map[string]any{"status": "completed", "usage": map[string]any{"input_tokens": 12, "output_tokens": 6, "input_tokens_details": map[string]int{"cached_tokens": 3}, "output_tokens_details": map[string]int{"reasoning_tokens": 2}}}})
}
func TestBuildNativeImagesToolsAndUsage(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/responses" || r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
			t.Error("wrong build transport")
		}
		for _, want := range []string{`"model":"grok-test"`, `"store":false`, `"stream":true`} {
			if !strings.Contains(string(b), want) {
				t.Error("lost", want)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			for _, want := range []string{"input_image", "data:image/png;base64,AQID", "lookup"} {
				if !strings.Contains(string(b), want) {
					t.Error("lost", want)
				}
			}
			io.WriteString(w, event("response.output_item.added", map[string]any{"item": map[string]string{"type": "function_call", "id": "item", "call_id": "call", "name": "lookup"}}))
			io.WriteString(w, event("response.function_call_arguments.done", map[string]string{"item_id": "item", "arguments": `{"n":9007199254740993}`}))
		} else {
			for _, want := range []string{"function_call_output", "RESULT", "9007199254740993"} {
				if !strings.Contains(string(b), want) {
					t.Error("lost history", want)
				}
			}
			io.WriteString(w, event("response.output_text.delta", map[string]string{"delta": "GROK_OK"}))
		}
		io.WriteString(w, completed())
	}))
	defer s.Close()
	c := New()
	c.BaseURL = s.URL
	var chunks [][]byte
	emit := func(b []byte) error { chunks = append(chunks, append([]byte(nil), b...)); return nil }
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)
	if err := c.Stream(context.Background(), fixture(t), provider.Request{Model: "grok-test", Body: body}, emit); err != nil {
		t.Fatal(err)
	}
	b, err := protocol.AggregateSSE(chunks)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Choices []struct {
			Message json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(b, &reply)
	body = []byte(`{"messages":[{"role":"user","content":"hi"},` + string(reply.Choices[0].Message) + `,{"role":"tool","tool_call_id":"call","content":"RESULT"}]}`)
	chunks = nil
	if err = c.Stream(context.Background(), fixture(t), provider.Request{Model: "grok-test", Body: body}, emit); err != nil {
		t.Fatal(err)
	}
	b, err = protocol.AggregateSSE(chunks)
	for _, want := range []string{"GROK_OK", `"cached_tokens":3`, `"reasoning_tokens":2`, `"completion_tokens":6`} {
		if err != nil || !strings.Contains(string(b), want) {
			t.Fatal(string(b), err)
		}
	}
}
func TestBuildStreamFailureAndCancellation(t *testing.T) {
	for _, body := range []string{"", event("response.completed", map[string]any{"response": map[string]any{"status": "completed", "output": []any{}}}), event("response.output_text.delta", map[string]string{"delta": "partial"}), event("response.failed", map[string]string{"error": "private"})} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, body)
		}))
		c := New()
		c.BaseURL = s.URL
		err := c.Stream(context.Background(), fixture(t), provider.Request{Model: "grok-test", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
		s.Close()
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, event("response.output_text.delta", map[string]string{"delta": "hello"})+completed())
	}))
	defer s.Close()
	c := New()
	c.BaseURL = s.URL
	request := provider.Request{Model: "grok-test", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
	stop := errors.New("client stopped")
	if err := c.Stream(context.Background(), fixture(t), request, func([]byte) error { return stop }); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Stream(ctx, fixture(t), request, func([]byte) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNativeWebSearch(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Tools []struct{ Type string } }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Tools) != 1 || body.Tools[0].Type != "web_search" {
			t.Error("missing native search tool")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, event("response.created", map[string]any{"response": map[string]string{"status": "in_progress"}}))
		io.WriteString(w, event("response.output_item.done", map[string]any{"item": map[string]string{"type": "web_search_call"}}))
		io.WriteString(w, completed())
	}))
	defer s.Close()
	c := New()
	c.BaseURL = s.URL
	search := false
	err := c.StreamNative(context.Background(), fixture(t), provider.Request{Model: "grok-test", Protocol: provider.ResponsesProtocol, Body: json.RawMessage(`{"input":"query","tools":[{"type":"web_search"}]}`)}, func(raw []byte) error {
		if strings.Contains(string(raw), "web_search_call") {
			search = true
		}
		return nil
	})
	if err != nil || !search {
		t.Fatalf("search event=%v err=%v", search, err)
	}
}
