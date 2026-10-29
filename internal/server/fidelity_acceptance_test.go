package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/store"
)

// Verify business data after the entire gateway/provider request path, rather
// than only testing the cleaner in isolation. All credentials/upstreams are local fixtures.
func TestFidelityBusinessDataAtEachUpstream(t *testing.T) {
	const userText = "  User data: 11128; cc_version=42;\nx-anthropic-billing-header: business value;\nYou are Claude Code, Anthropic's official CLI for Claude.  "
	const toolText = "  Tool result: cc_version=42; 11128\nx-anthropic-billing-header: keep;  "
	const args = `{"path":"11128.txt","content":"cc_version=42; 11128\nYou are Claude Code, Anthropic's official CLI for Claude.","n":9007199254740993}`
	const systemText = "x-anthropic-billing-header: cc_version=client;\nYou are Claude Code, Anthropic's official CLI for Claude.\nKeep system ID 11128."
	const cleanSystem = "You are Claude Code, Anthropic's official CLI tool for Claude.\nKeep system ID 11128."
	for _, name := range []string{"codearts", "workbuddy", "trae"} {
		for _, mode := range []string{"passthrough", "custom"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%v", name, mode, stream), func(t *testing.T) {
					calls := 0
					handler := func(w http.ResponseWriter, r *http.Request) {
						calls++
						var body struct {
							Stream   bool
							Messages []struct {
								Role       string
								Content    json.RawMessage
								ToolCallID string `json:"tool_call_id"`
								Calls      []struct {
									ID       string
									Function struct{ Name, Arguments string }
									Native   struct{ Name, Arguments string } `json:"function_call"`
								} `json:"tool_calls"`
							}
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Fatal(err)
						}
						text := func(raw json.RawMessage) string {
							var s string
							if json.Unmarshal(raw, &s) == nil {
								return s
							}
							var parts []struct{ Type, Text string }
							if err := json.Unmarshal(raw, &parts); err != nil {
								t.Fatal(err)
							}
							for _, p := range parts {
								if p.Type == "text" {
									s += p.Text
								}
							}
							return s
						}
						if len(body.Messages) != 4 {
							t.Fatalf("messages changed: %+v", body.Messages)
						}
						wantSystem := cleanSystem
						if mode == "custom" {
							wantSystem = "ADMIN PROMPT 11128 cc_version=42;"
						}
						if text(body.Messages[0].Content) != wantSystem {
							t.Errorf("unexpected system: %q", text(body.Messages[0].Content))
						}
						if body.Messages[1].Role != "user" || text(body.Messages[1].Content) != userText {
							t.Error("user data changed")
						}
						history := body.Messages[2]
						if history.Role != "assistant" || len(history.Calls) != 1 {
							t.Fatal("tool call history lost")
						}
						call := history.Calls[0]
						fn := call.Function
						if name == "trae" {
							fn = call.Native
						}
						if call.ID != "call-11128" || fn.Name != "write" || fn.Arguments != args {
							t.Errorf("tool arguments changed: %q", fn.Arguments)
						}
						if body.Messages[3].Role != "tool" || body.Messages[3].ToolCallID != "call-11128" || text(body.Messages[3].Content) != toolText {
							t.Error("tool result changed")
						}
						if name == "workbuddy" {
							w.Header().Set("Content-Type", "text/event-stream")
							io.WriteString(w, summaryFixture("OK", "stop", 12, 4))
						} else {
							policySuccess(w, name, body.Stream)
						}
					}
					var s *Server
					var key, model, acct string
					if name == "workbuddy" {
						s, key, _ = nativeWBServer(t, handler)
						addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
						model = "workbuddy/cn/shared"
						acct = "wb-cn"
					} else {
						s, key, model, acct = providerPolicyServer(t, name, handler)
					}
					s.Store.UpdateSettings(func(st *store.Settings) {
						p := &st.CodeArts
						if name == "workbuddy" {
							p = &st.WorkBuddy
						}
						if name == "trae" {
							p = &st.Trae
						}
						p.PromptMode = mode
						p.PromptText = "ADMIN PROMPT 11128 cc_version=42;"
						p.Sanitize = true
					})
					req := map[string]any{"model": model, "stream": stream, "messages": []any{
						map[string]any{"role": "system", "content": systemText},
						map[string]any{"role": "user", "content": userText},
						map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call-11128", "type": "function", "function": map[string]any{"name": "write", "arguments": args}}}},
						map[string]any{"role": "tool", "tool_call_id": "call-11128", "content": toolText},
					}}
					w := responseRequest(s, key, "/v1/chat/completions", string(mustJSON(req)))
					if w.Code != 200 || calls != 1 {
						t.Fatalf("completion/retry: status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
					}
					log := s.Store.Logs(1)[0]
					if log.Attempts != 1 || log.Provider != name || !log.Usage.Known {
						t.Fatalf("unexpected accounting: %+v", log)
					}
					if s.Store.PoolSnapshot()[acct].InFlight != 0 {
						t.Fatal("lease leaked")
					}
					if strings.Contains(w.Body.String(), `"error"`) {
						t.Fatalf("stream error: %s", w.Body.String())
					}
				})
			}
		}
	}
}
