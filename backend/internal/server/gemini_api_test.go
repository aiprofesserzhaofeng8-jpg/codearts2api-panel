package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/upstream/gemini"
)

func TestGeminiSharedChatSignedToolHistory(t *testing.T) {
	s, key, count := geminiSignedFixture(t, true)
	body := `{"model":"gemini-3-flash-preview","messages":[{"role":"user","content":[{"type":"text","text":"lookup"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`
	first := responseRequest(s, key, "/v1/chat/completions", body)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var reply struct {
		Choices []struct {
			Message json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(first.Body.Bytes(), &reply) != nil || len(reply.Choices) != 1 || !strings.Contains(first.Body.String(), "SIGNED_TOOL") {
		t.Fatal(first.Body.String())
	}
	second := responseRequest(s, key, "/v1/chat/completions", `{"model":"gemini-3-flash-preview","messages":[{"role":"user","content":"lookup"},`+string(reply.Choices[0].Message)+`,{"role":"tool","tool_call_id":"tool-g","content":"TOOL_RESULT"}]}`)
	if second.Code != 200 || !strings.Contains(second.Body.String(), "GEMINI_TOOL_OK") || *count != 2 {
		t.Fatal(second.Code, second.Body.String(), *count)
	}
	logs := s.Store.Logs(2)
	if len(logs) != 2 || logs[0].Provider != "gemini" || logs[0].Usage.InputTokens != 20 || logs[0].Usage.ReasoningTokens != 3 || logs[0].Usage.CachedTokens != 5 || s.Store.PoolSnapshot()["gemini-fixture"].InFlight != 0 {
		t.Fatal(logs)
	}
}

func geminiSignedFixture(t *testing.T, requireImage bool) (*Server, string, *int) {
	t.Helper()
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" || !strings.Contains(string(body), `"gemini-3-flash-preview"`) {
			t.Error("wrong provider request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			if requireImage && !strings.Contains(string(body), "image/png") {
				t.Error("lost image")
			}
			io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"tool-g","type":"function","function":{"name":"lookup","arguments":"{\"n\":9007199254740993}"},"extra_content":{"google":{"thought_signature":"SIGNED_TOOL"}}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			if !strings.Contains(string(body), "SIGNED_TOOL") || !strings.Contains(string(body), "TOOL_RESULT") || !strings.Contains(string(body), "9007199254740993") {
				t.Error("gateway lost signed tool history")
			}
			io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"GEMINI_TOOL_OK"},"finish_reason":"stop"}]}`+"\n\n")
		}
		io.WriteString(w, "data: "+`{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens_details":{"reasoning_tokens":3}}}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(up.Close)
	s := newAdminTestServer(t)
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := gemini.New()
	c.Base = up.URL
	s.Providers = provider.NewRegistry()
	if err := s.Providers.Register(c); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"api_key":"fixture-key"}`)
	_, id, _ := c.Normalize(raw)
	s.Store.UpsertAccount(&accounts.Account{ID: "gemini-fixture", Provider: "gemini", Scope: "api", ExternalID: id.ID, Credentials: raw, Enabled: true})
	s.Reg.ReplaceProvider("gemini", "api", []provider.Model{{ID: "gemini-3-flash-preview", ImageInput: "supported", ToolCalling: "supported"}})
	_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "gemini-3-flash-preview", Enabled: true, Sources: []config.ModelSource{{ID: "gemini/api/gemini-3-flash-preview", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	key := s.Store.AddKey("fixture").Key
	_ = s.Handler()
	markModelsFresh(s)

	return s, key, &calls
}

func TestGeminiSignedToolsAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", path, stream), func(t *testing.T) {
				s, key, count := geminiSignedFixture(t, false)
				var firstBody string
				if path == "/v1/messages" {
					firstBody = fmt.Sprintf(`{"model":"gemini-3-flash-preview","max_tokens":128,"stream":%v,"messages":[{"role":"user","content":"lookup"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`, stream)
				} else {
					firstBody = fmt.Sprintf(`{"model":"gemini-3-flash-preview","stream":%v,"input":"lookup","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`, stream)
				}
				first := responseRequest(s, key, path, firstBody)
				if first.Code != 200 {
					t.Fatal(first.Code, first.Body.String())
				}
				var next string
				if path == "/v1/messages" {
					history := historyContent(t, first.Body.String(), stream)
					if !strings.Contains(string(history), "SIGNED_TOOL") {
						t.Fatal("lost tool metadata", string(history))
					}
					next = fmt.Sprintf(`{"model":"gemini-3-flash-preview","max_tokens":128,"stream":%v,"messages":[{"role":"user","content":"lookup"},{"role":"assistant","content":%s},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-g","content":"TOOL_RESULT"}]}]}`, stream, history)
				} else {
					var output []json.RawMessage
					if stream {
						for _, e := range responseEvents(t, first.Body.String()) {
							if (e.Type == "response.output_item.added" || e.Type == "response.output_item.done") && e.Item != nil && e.Item.Type == "function_call" && !strings.Contains(string(e.Item.ExtraContent), "SIGNED_TOOL") {
								t.Fatal("stream item lost metadata", e.Type)
							}
							if e.Type == "response.completed" && e.Response != nil {
								for _, item := range e.Response.Output {
									output = append(output, mustJSON(item))
								}
							}
						}
					} else {
						var result struct {
							Output []json.RawMessage `json:"output"`
						}
						if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil {
							t.Fatal(err)
						}
						output = result.Output
					}
					if !strings.Contains(string(mustJSON(output)), "SIGNED_TOOL") {
						t.Fatal("lost tool metadata", string(mustJSON(output)))
					}
					input := []json.RawMessage{json.RawMessage(`{"role":"user","content":"lookup"}`)}
					input = append(input, output...)
					input = append(input, json.RawMessage(`{"type":"function_call_output","call_id":"tool-g","output":"TOOL_RESULT"}`))
					next = fmt.Sprintf(`{"model":"gemini-3-flash-preview","stream":%v,"input":%s}`, stream, mustJSON(input))
				}
				second := responseRequest(s, key, path, next)
				if second.Code != 200 || !strings.Contains(second.Body.String(), "GEMINI_TOOL_OK") || *count != 2 {
					t.Fatal(second.Code, second.Body.String(), *count)
				}
				if s.Store.PoolSnapshot()["gemini-fixture"].InFlight != 0 {
					t.Fatal("lease leak")
				}
			})
		}
	}
}
