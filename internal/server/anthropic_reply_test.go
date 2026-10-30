package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/registry"
)

func TestMessagesReplyFieldsAtHTTPBoundary(t *testing.T) {
	for _, upstream := range []string{"codearts", "codearts-sse", "workbuddy", "trae"} {
		for _, stream := range []bool{false, true} {
			for _, content := range []string{"text", "refusal", "mixed"} {
				if upstream == "trae" && content != "text" {
					continue // Native Trae has no verified refusal-field fixture.
				}
				t.Run(fmt.Sprintf("%s/stream=%v/%s", upstream, stream, content), func(t *testing.T) {
					text, refusal, thinking, finish := "NORMAL_11128", "", "", "stop"
					var calls atomic.Int32
					var tools []any
					if content == "refusal" {
						text, refusal = "", "拒答_EXACT"
					} else if content == "mixed" {
						thinking, refusal, finish = "THINK_11128", "REFUSAL_11128", "tool_calls"
						tools = []any{map[string]any{"index": 0, "id": "call_1", "type": "function", "function": map[string]string{"name": "lookup", "arguments": `{"id":9007199254740993}`}}}
					}
					handler := func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						var req struct{ Stream bool }
						_ = json.NewDecoder(r.Body).Decode(&req)
						if upstream == "trae" {
							w.Header().Set("Content-Type", "text/event-stream")
							io.WriteString(w, "event: output\ndata: {\"response\":\"NORMAL_11128\"}\n\nevent: token_usage\ndata: {\"prompt_tokens\":12,\"completion_tokens\":2,\"cached_tokens\":8}\n\nevent: done\ndata: {\"finish_reason\":\"stop\"}\n\n")
							return
						}
						message := map[string]any{"content": text, "refusal": refusal, "reasoning_content": thinking}
						if content == "refusal" {
							message["content"] = nil
						}
						if len(tools) > 0 {
							message["tool_calls"] = tools
						}
						usage := map[string]any{"prompt_tokens": 12, "completion_tokens": 2, "prompt_tokens_details": map[string]int{"cached_tokens": 8}}
						if req.Stream {
							w.Header().Set("Content-Type", "text/event-stream")
							first := mustJSON(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": message}}})
							last := mustJSON(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, "usage": usage})
							fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", first, last)
						} else {
							w.Header().Set("Content-Type", "application/json")
							w.Write(mustJSON(map[string]any{"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": usage}))
						}
					}
					providerName := upstream
					if strings.HasPrefix(upstream, "codearts") {
						providerName = "codearts"
					}
					s, key, model, account := interruptionServer(t, providerName, handler)
					if providerName == "codearts" {
						s.Reg.Replace([]registry.Model{{ID: model, StreamOnly: upstream == "codearts-sse"}})
					}
					gateway := httptest.NewServer(s.Handler())
					defer gateway.Close()
					req, _ := http.NewRequest("POST", gateway.URL+"/v1/messages", strings.NewReader(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"max_tokens":64,"stream":%v}`, model, stream)))
					req.Header.Set("Authorization", "Bearer "+key)
					res, err := gateway.Client().Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer res.Body.Close()
					raw, err := io.ReadAll(res.Body)
					if err != nil || res.StatusCode != 200 || calls.Load() != 1 {
						t.Fatalf("status=%d calls=%d err=%v body=%s", res.StatusCode, calls.Load(), err, raw)
					}
					wantStop := "end_turn"
					if content == "mixed" {
						wantStop = "tool_use"
						if !strings.Contains(string(raw), "9007199254740993") {
							t.Fatal("tool argument number changed")
						}
					}
					var actualText, actualThinking, actualStop string
					var usage map[string]int
					if stream {
						_, stop, failed := readAnthropicBlocks(t, string(raw))
						actualStop = stop
						if failed {
							t.Fatal("valid response became an SSE error")
						}
						for _, line := range strings.Split(string(raw), "\n") {
							if !strings.HasPrefix(line, "data: ") {
								continue
							}
							var event struct {
								Type  string
								Delta struct{ Text, Thinking string }
								Usage map[string]int
							}
							if err := json.Unmarshal([]byte(line[6:]), &event); err != nil {
								t.Fatal(err)
							}
							actualText += event.Delta.Text
							actualThinking += event.Delta.Thinking
							if event.Type == "message_delta" {
								usage = event.Usage
							}
						}
					} else {
						var reply struct {
							Content []struct{ Text, Thinking string }
							Stop    string `json:"stop_reason"`
							Usage   map[string]int
						}
						if err := json.Unmarshal(raw, &reply); err != nil {
							t.Fatal(err)
						}
						for _, block := range reply.Content {
							actualText += block.Text
							actualThinking += block.Thinking
						}
						actualStop, usage = reply.Stop, reply.Usage
					}
					if actualText != text+refusal || actualThinking != thinking || actualStop != wantStop || usage["input_tokens"] != 4 || usage["cache_read_input_tokens"] != 8 || usage["output_tokens"] != 2 {
						t.Fatalf("reply fields changed: text=%q thinking=%q stop=%s usage=%v", actualText, actualThinking, actualStop, usage)
					}
					log := s.Store.Logs(1)[0]
					if log.Status != 200 || log.Attempts != 1 || !log.Usage.Known || log.Usage.InputTokens != 12 || log.Usage.CachedTokens != 8 || log.Usage.OutputTokens != 2 || log.OutputLen != len([]rune(text+refusal+thinking)) || s.Store.PoolSnapshot()[account].InFlight != 0 {
						t.Fatalf("client mapping altered internal accounting or leaked lease: %+v", log)
					}
				})
			}
		}
	}
}

func TestMessagesRefusalStreamsBeforeUpstreamFinishes(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	defer open()
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()
			io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"refusal\":\"REFUSAL_FIRST\"}}]}\n\n")
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader, Request: r}, nil
	})
	gateway := httptest.NewServer(s.Handler())
	defer func() { open(); gateway.Close() }()
	client := gateway.Client()
	client.Timeout = 3 * time.Second
	req, _ := http.NewRequest("POST", gateway.URL+"/v1/messages", strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"stream":true}`))
	req.Header.Set("Authorization", "Bearer "+key)
	done := make(chan error, 1)
	go func() {
		res, err := client.Do(req)
		if err != nil {
			done <- err
			return
		}
		defer res.Body.Close()
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "REFUSAL_FIRST") {
				open()
				_, err := io.Copy(io.Discard, res.Body)
				done <- err
				return
			}
		}
		done <- fmt.Errorf("missing refusal delta: %v", scanner.Err())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		open()
		t.Fatal("refusal buffered until upstream completion")
	}
}
