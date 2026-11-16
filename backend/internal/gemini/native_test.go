package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/protocol"
	"codearts2api/internal/provider"
)

func TestCLINativeToolImageAndPrecision(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"SYSTEM"},{"role":"user","content":[{"type":"text","text":"QUESTION"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID"}}]},{"role":"assistant","tool_calls":[{"id":"call1","function":{"name":"lookup","arguments":"{\"n\":9007199254740993}"},"extra_content":{"google":{"thought_signature":"SIGNED"}}}]},{"role":"tool","tool_call_id":"call1","content":"RESULT"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"n":{"type":"integer"}},"additionalProperties":false}}}],"tool_choice":"required","max_tokens":123,"reasoning_effort":"low"}`)
	b, err := nativeRequest(body, "gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"systemInstruction", "SYSTEM", "inlineData", "AQID", "functionCall", "9007199254740993", "thoughtSignature", "SIGNED", "functionResponse", "RESULT", "functionDeclarations", "ANY", "maxOutputTokens", "thinkingBudget"} {
		if !strings.Contains(string(b), want) {
			t.Fatal("lost", want, string(b))
		}
	}
	if strings.Contains(string(b), "additionalProperties") {
		t.Fatal("schema was not adapted")
	}
	for _, bad := range []string{`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://127.0.0.1/private"}}]}]}`, `{"messages":[{"role":"tool","tool_call_id":"orphan","content":"x"}]}`, `{"messages":[{"role":"user","content":[{"type":"input_audio"}]}]}`, `{"messages":[{"role":"user","content":"hi"}],"extra_body":{}}`} {
		if _, err := nativeRequest([]byte(bad), "gemini-3-flash"); err == nil {
			t.Fatal("silently lost unsupported input")
		}
	}
}

func TestCLINativeStreamAndHistory(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1internal:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" || r.Header.Get("Authorization") != "Bearer old-token" || r.Header.Get("User-Agent") != GeminiCLIUserAgent {
			t.Error("wrong CLI endpoint/auth")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"project":"project-1"`) || !strings.Contains(string(body), `"model":"gemini-3-flash"`) {
			t.Error("lost wrapper")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			io.WriteString(w, "data: "+`{"response":{"candidates":[{"index":0,"content":{"parts":[{"functionCall":{"name":"lookup","args":{"n":9007199254740993}},"thoughtSignature":"SIGNED"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":4,"thoughtsTokenCount":3,"cachedContentTokenCount":5}}}`+"\n\n")
		} else {
			for _, want := range []string{"SIGNED", "9007199254740993", "RESULT", "functionResponse"} {
				if !strings.Contains(string(body), want) {
					t.Error("lost history", want)
				}
			}
			io.WriteString(w, "data: "+`{"response":{"candidates":[{"content":{"parts":[{"text":"CLI_OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":4,"thoughtsTokenCount":3,"cachedContentTokenCount":5}}}`+"\n\n")
		}
	}))
	defer up.Close()
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.CLIBase = up.URL
	var chunks [][]byte
	emit := func(b []byte) error { chunks = append(chunks, append([]byte(nil), b...)); return nil }
	if err := c.Stream(context.Background(), cliFixture(), provider.Request{Model: "gemini-3-flash", Body: []byte(`{"messages":[{"role":"user","content":"lookup"}]}`)}, emit); err != nil {
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
	var msg struct {
		Calls []struct {
			ID string `json:"id"`
		} `json:"tool_calls"`
	}
	json.Unmarshal(reply.Choices[0].Message, &msg)
	follow := []byte(`{"messages":[{"role":"user","content":"lookup"},` + string(reply.Choices[0].Message) + `,{"role":"tool","tool_call_id":"` + msg.Calls[0].ID + `","content":"RESULT"}]}`)
	chunks = nil
	if err := c.Stream(context.Background(), cliFixture(), provider.Request{Model: "gemini-3-flash", Body: follow}, emit); err != nil {
		t.Fatal(err)
	}
	b, err = protocol.AggregateSSE(chunks)
	if err != nil || !strings.Contains(string(b), "CLI_OK") || !strings.Contains(string(b), `"completion_tokens":7`) || !strings.Contains(string(b), `"reasoning_tokens":3`) {
		t.Fatal(string(b), err)
	}
}

func TestCLINativeNeverAcceptsEmptyOrBrokenStreams(t *testing.T) {
	for _, fixture := range []string{`{}`, `{"error":{"message":"secret"}}`, `{"response":{"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}}`, `{"response":{"candidates":[{"finishReason":"STOP"}]}}`, `{"response":{"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"MALFORMED_FUNCTION_CALL"}]}}`} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: "+fixture+"\n\n")
		}))
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
		c := New()
		c.CLIBase = up.URL
		err := c.Stream(context.Background(), cliFixture(), provider.Request{Model: "gemini-3-flash", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
		up.Close()
	}
}

func TestCLINativeCancellationErrorsAndEmptyToolArgs(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 302} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(status)
			io.WriteString(w, `{"secret":"do-not-leak"}`)
		}))
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
		c := New()
		c.CLIBase = up.URL
		err := c.Stream(context.Background(), cliFixture(), provider.Request{Model: "gemini-3-flash", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
		if err == nil || strings.Contains(err.Error(), "do-not-leak") {
			t.Fatal(err)
		}
		up.Close()
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: "+`{"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"now"}}]},"finishReason":"STOP"}]}}`+"\n\n")
	}))
	defer up.Close()
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.CLIBase = up.URL
	request := provider.Request{Model: "gemini-3-flash", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
	var chunks [][]byte
	if err := c.Stream(context.Background(), cliFixture(), request, func(b []byte) error { chunks = append(chunks, append([]byte(nil), b...)); return nil }); err != nil {
		t.Fatal(err)
	}
	b, err := protocol.AggregateSSE(chunks)
	if err != nil || !strings.Contains(string(b), `"arguments":"{}"`) {
		t.Fatal(string(b), err)
	}
	expected := errors.New("client stopped")
	if err := c.Stream(context.Background(), cliFixture(), request, func([]byte) error { return expected }); !errors.Is(err, expected) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Stream(ctx, cliFixture(), request, func([]byte) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
