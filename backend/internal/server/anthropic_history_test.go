package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// Reconstruct the exact Messages history a client sends after streaming output.
func historyContent(t *testing.T, raw string, stream bool) json.RawMessage {
	t.Helper()
	if !stream {
		var reply struct{ Content json.RawMessage }
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatal(err)
		}
		return reply.Content
	}
	blocks := []map[string]json.RawMessage{}
	args := map[int]string{}
	terminal := false
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var e struct {
			Type  string
			Index int
			Block map[string]json.RawMessage `json:"content_block"`
			Delta struct {
				Type, Thinking, Text string
				Partial              string `json:"partial_json"`
			}
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e); err != nil {
			t.Fatal(err)
		}
		switch e.Type {
		case "content_block_start":
			if e.Index != len(blocks) {
				t.Fatal("non-contiguous content block")
			}
			blocks = append(blocks, e.Block)
		case "content_block_delta":
			if e.Delta.Type == "input_json_delta" {
				args[e.Index] += e.Delta.Partial
				continue
			}
			field, value := "text", e.Delta.Text
			if e.Delta.Type == "thinking_delta" {
				field, value = "thinking", e.Delta.Thinking
			}
			var before string
			_ = json.Unmarshal(blocks[e.Index][field], &before)
			blocks[e.Index][field], _ = json.Marshal(before + value)
		case "message_stop":
			terminal = true
		}
	}
	if !terminal {
		t.Fatal("missing message_stop", raw)
	}
	for i, a := range args {
		blocks[i]["input"] = json.RawMessage(a)
	}
	return mustJSON(blocks)
}

func TestMessagesUnsignedThinkingToolRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, refuse := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/refuse=%v", stream, refuse), func(t *testing.T) {
				calls := 0
				s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					var req struct{ Messages []map[string]json.RawMessage }
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatal(err)
					}
					delta := map[string]any{}
					finish := "stop"
					if calls == 1 {
						delta = map[string]any{"reasoning_content": "EXACT reason\n11128", "tool_calls": []any{map[string]any{"index": 0, "id": "call-A", "type": "function", "function": map[string]string{"name": "lookup", "arguments": `{"n":9007199254740993}`}}}}
						finish = "tool_calls"
					} else {
						if len(req.Messages) != 3 {
							t.Fatal("history changed", len(req.Messages))
						}
						var thought string
						_ = json.Unmarshal(req.Messages[1]["reasoning_content"], &thought)
						if thought != "EXACT reason\n11128" || !strings.Contains(string(req.Messages[1]["tool_calls"]), "9007199254740993") || string(req.Messages[2]["tool_call_id"]) != `"call-A"` || string(req.Messages[2]["content"]) != `"SYNTHETIC_RESULT"` {
							t.Error("thinking or tool history lost", req.Messages)
						}
						if refuse {
							delta["refusal"] = "DECLINED"
						} else {
							delta["content"] = "SYNTHETIC_RESULT"
						}
					}
					w.Header().Set("Content-Type", "text/event-stream")
					first := mustJSON(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
					last := mustJSON(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 4}})
					fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", first, last)
				})
				addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
				s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "shared", Name: "shared", ToolCalling: "supported", StreamOnly: true}})
				_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "history-v1", Enabled: true, Sources: []config.ModelSource{{ID: "workbuddy/cn/shared", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
				if err != nil {
					t.Fatal(err)
				}
				markModelsFresh(s)
				body := fmt.Sprintf(`{"model":"history-v1","max_tokens":128,"stream":%v,"messages":[{"role":"user","content":"lookup"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`, stream)
				first := responseRequest(s, key, "/v1/messages", body)
				if first.Code != 200 {
					t.Fatal(first.Code, first.Body.String())
				}
				history := historyContent(t, first.Body.String(), stream)
				if !strings.Contains(string(history), `"thinking"`) {
					t.Fatal("fixture did not return thinking")
				}
				body = fmt.Sprintf(`{"model":"history-v1","max_tokens":128,"stream":%v,"messages":[{"role":"user","content":"lookup"},{"role":"assistant","content":%s},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-A","content":"SYNTHETIC_RESULT"}]}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`, stream, history)
				second := responseRequest(s, key, "/v1/messages", body)
				expected := "SYNTHETIC_RESULT"
				if refuse {
					expected = "DECLINED"
				}
				if second.Code != 200 || !strings.Contains(string(historyContent(t, second.Body.String(), stream)), expected) || calls != 2 {
					t.Fatal(second.Code, second.Body.String(), calls)
				}
				if s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
					t.Fatal("lease leaked")
				}
			})
		}
	}
}

func TestMessagesSignedHistoryNeverFallsBackToChat(t *testing.T) {
	for _, model := range []string{"workbuddy/cn/shared", "history-v1"} {
		for _, block := range []string{`{"type":"thinking","thinking":"reason","signature":"SIGNED"}`, `{"type":"redacted_thinking","data":"OPAQUE"}`} {
			t.Run(model+block, func(t *testing.T) {
				calls := 0
				s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) { calls++; io.WriteString(w, "unexpected") })
				addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
				_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "history-v1", Enabled: true, Sources: []config.ModelSource{{ID: "workbuddy/cn/shared", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
				if err != nil {
					t.Fatal(err)
				}
				markModelsFresh(s)
				body := fmt.Sprintf(`{"model":%q,"max_tokens":128,"messages":[{"role":"assistant","content":[%s]},{"role":"user","content":"continue"}]}`, model, block)
				w := responseRequest(s, key, "/v1/messages", body)
				if w.Code != 400 || calls != 0 || s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
					t.Fatal("native history crossed incompatible model", w.Code, calls, w.Body.String())
				}
			})
		}
	}
}
