package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/registry"
	apicompat "codearts2api/internal/responsescompat"
)

func responseTestServer(t *testing.T, upstream func(*http.Request) (*http.Response, error)) (*Server, string) {
	t.Helper()
	s := newTestServer(t, 1, 2)
	s.Cred = codearts.NewManager(s.Up)
	s.Reg.Replace([]registry.Model{{ID: "test-model"}})
	s.Store.SetAccountSession("a", &codearts.Session{AccessKey: "ak", SecretKey: "sk", SecurityToken: "st", ExpiresAt: time.Now().Add(time.Hour)})
	key := s.Store.AddKey("responses test").Key
	s.Up.HTTP = &http.Client{Transport: roundTripFunc(upstream)}
	return s, key
}
func responseUpstream(r *http.Request, data string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data)), Request: r}
}
func responseRequest(s *Server, key, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

const responsesTextFixture = `data: {"choices":[{"index":0,"delta":{"reasoning_content":"thinking"}}]}

data: {"choices":[{"index":0,"delta":{"content":"Hello"}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":2}}}

data: [DONE]

`

func responseEvents(t *testing.T, body string) []apicompat.ResponsesStreamEvent {
	t.Helper()
	var out []apicompat.ResponsesStreamEvent
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.SequenceNumber != len(out) {
			t.Fatalf("event sequence %d, want %d", event.SequenceNumber, len(out))
		}
		out = append(out, event)
	}
	return out
}
func TestResponsesBufferedAndStreaming(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != codearts.ChatPath {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["stream"] != true || body["max_tokens"] != float64(128) || body["max_completion_tokens"] != nil {
					t.Errorf("wrong upstream options: %v", body)
				}
				messages := body["messages"].([]any)
				if messages[0].(map[string]any)["role"] != "system" {
					t.Error("instructions lost")
				}
				return responseUpstream(r, responsesTextFixture), nil
			})
			w := responseRequest(s, key, "/v1/responses", fmt.Sprintf(`{"model":"test-model","instructions":"Be brief","input":"Hi","max_output_tokens":128,"stream":%v,"store":false}`, stream))
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			var result *apicompat.ResponsesResponse
			if stream {
				events := responseEvents(t, w.Body.String())
				if len(events) == 0 || events[0].Type != "response.created" || events[len(events)-1].Type != "response.completed" {
					t.Fatalf("bad lifecycle: %s", w.Body.String())
				}
				result = events[len(events)-1].Response
				if !strings.HasSuffix(w.Body.String(), "data: [DONE]\n\n") {
					t.Error("missing terminal sentinel")
				}
			} else {
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
			}
			if result.Status != "completed" || result.CreatedAt == 0 || len(result.Output) != 2 || result.Output[1].Content[0].Text != "Hello" {
				t.Fatalf("bad response: %+v", result)
			}
			if result.Usage == nil || result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 4 {
				t.Fatalf("missing usage: %+v", result.Usage)
			}
			if got := s.responsesReasoning.get(responsesCacheScope(key), result.Output[0].ID, time.Now()); got != "thinking" {
				t.Errorf("reasoning not cached: %q", got)
			}
		})
	}
}

func TestResponsesToolRoundTripAndReasoning(t *testing.T) {
	var calls atomic.Int32
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return responseUpstream(r, `data: {"choices":[{"index":0,"delta":{"reasoning_content":"Need tool"}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`), nil
		}
		var body apicompat.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		foundTool, foundReasoning := false, false
		for _, m := range body.Messages {
			if m.Role == "tool" && m.ToolCallID == "call_1" && strings.Contains(string(m.Content), "sunny") {
				foundTool = true
			}
			if len(m.ToolCalls) > 0 && m.ReasoningContent == "Need tool" {
				foundReasoning = true
			}
		}
		if !foundTool || !foundReasoning {
			t.Errorf("lost history: %+v", body.Messages)
		}
		return responseUpstream(r, responsesTextFixture), nil
	})
	tools := `[{"type":"function","name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]`
	first := responseRequest(s, key, "/responses", `{"model":"test-model","input":"weather?","stream":true,"tools":`+tools+`}`)
	events := responseEvents(t, first.Body.String())
	result := events[len(events)-1].Response
	if result == nil || len(result.Output) != 2 {
		t.Fatalf("unexpected tools: %s", first.Body.String())
	}
	call := result.Output[1]
	if call.Type != "function_call" || call.CallID != "call_1" || call.Arguments != `{"city":"Paris"}` {
		t.Fatalf("bad call: %+v", call)
	}
	// Simulate a client replaying only the reasoning id (plaintext omitted).
	result.Output[0].Summary = nil
	input := []any{map[string]any{"role": "user", "content": "weather?"}, result.Output[0], call, map[string]any{"type": "function_call_output", "call_id": call.CallID, "output": "sunny"}}
	raw, _ := json.Marshal(map[string]any{"model": "test-model", "input": input, "tools": json.RawMessage(tools)})
	next := responseRequest(s, key, "/v1/responses", string(raw))
	if next.Code != 200 {
		t.Fatalf("followup: %d %s", next.Code, next.Body.String())
	}
}

func TestResponsesFailedStreamIsNotCompletedOrReplayed(t *testing.T) {
	for _, fixture := range []string{
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_bad\",\"type\":\"function\",\"function\":{\"name\":\"test\",\"arguments\":\"{\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n",
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {bad json}\n\n",
	} {
		t.Run(fmt.Sprint(len(fixture)), func(t *testing.T) {
			var calls atomic.Int32
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) { calls.Add(1); return responseUpstream(r, fixture), nil })
			w := responseRequest(s, key, "/v1/responses", `{"model":"test-model","input":"hi","stream":true}`)
			events := responseEvents(t, w.Body.String())
			if len(events) == 0 || events[len(events)-1].Type != "response.failed" {
				t.Fatalf("no failure: %s", w.Body.String())
			}
			if strings.Contains(w.Body.String(), "response.completed") || strings.Contains(w.Body.String(), "[DONE]") {
				t.Fatal("reported success after failure")
			}
			if calls.Load() != 1 {
				t.Fatalf("replayed visible stream %d times", calls.Load())
			}
		})
	}
}

func TestResponsesValidationAndAuth(t *testing.T) {
	var calls atomic.Int32
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return responseUpstream(r, responsesTextFixture), nil
	})
	for _, extra := range []string{`"store":true`, `"previous_response_id":"resp_old"`, `"background":true`, `"conversation":"conv_old"`, `"tools":[{"type":"web_search"}]`, `"max_output_tokens":0`} {
		w := responseRequest(s, key, "/v1/responses", `{"model":"test-model","input":"hi",`+extra+`}`)
		if w.Code != 400 {
			t.Errorf("%s -> %d %s", extra, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/v1/responses", "/responses"} {
		w := responseRequest(s, "wrong", path, `{"model":"test-model","input":"hi"}`)
		if w.Code != 401 {
			t.Errorf("unauthorized %s: %d", path, w.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid/unauthorized requests reached upstream")
	}
}

func TestResponsesReasoningCacheIsolationExpiryAndBound(t *testing.T) {
	var c responsesReasoningCache
	now := time.Now()
	c.put("key-a", "item", "secret reasoning", now)
	if c.get("key-b", "item", now) != "" {
		t.Fatal("cache leaked between keys")
	}
	if c.get("key-a", "item", now.Add(responsesReasoningTTL)) != "" {
		t.Fatal("expired cache hit")
	}
	for i := 0; i < responsesReasoningMaxItems+10; i++ {
		c.put("key", fmt.Sprint(i), "reason", now)
	}
	if len(c.entries) > responsesReasoningMaxItems || c.bytes > responsesReasoningMaxBytes {
		t.Fatal("cache exceeded bounds")
	}
}
