package server

import (
	"codearts2api/internal/accounts"

	"codearts2api/internal/config"

	"codearts2api/internal/antigravity"

	"codearts2api/internal/provider"
	"codearts2api/internal/storage"

	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAntigravityProtocolAccounting(t *testing.T) {
	for _, factory := range []func() *antigravity.Client{antigravity.New} {
		t.Run(factory().Describe().ID, func(t *testing.T) {
			for _, public := range []bool{false, true} {
				for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
					for _, stream := range []bool{false, true} {
						t.Run(fmt.Sprintf("public=%v/%s/stream=%v", public, path, stream), func(t *testing.T) {
							calls := 0
							up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								calls++
								var body map[string]json.RawMessage
								_ = json.NewDecoder(r.Body).Decode(&body)
								var options map[string]json.RawMessage
								_ = json.Unmarshal(body["stream_options"], &options)
								if r.URL.Path != "/v1internal:streamGenerateContent" || r.Header.Get("Authorization") != "Bearer fixture-key" || string(body["model"]) != `"fixture-model"` || string(body["project"]) != `"fixture-project"` {
									t.Error("wrong upstream route or missing usage request")
								}
								w.Header().Set("Content-Type", "text/event-stream")
								io.WriteString(w, "data: "+`{"response":{"candidates":[{"content":{"parts":[{"text":"API_FIXTURE_OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":4,"cachedContentTokenCount":3}}}`+"\n\n")
							}))
							defer up.Close()
							s := newAdminTestServer(t)
							c := factory()
							c.BaseURL = up.URL
							s.Providers = provider.NewRegistry()
							if err := s.Providers.Register(c); err != nil {
								t.Fatal(err)
							}
							raw := json.RawMessage(fmt.Sprintf(`{"access_token":"fixture-key","refresh_token":"fixture-refresh","subject":"fixture-user","project_id":"fixture-project","scope":"global","expires_at":%d}`, time.Now().Unix()+3600))

							_, identity, err := c.Normalize(raw)
							if err != nil {
								t.Fatal(err)
							}
							s.Store.UpsertAccount(&accounts.Account{ID: "mc", Provider: c.Describe().ID, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
							s.Reg.ReplaceProvider(c.Describe().ID, identity.Scope, []provider.Model{{ID: "fixture-model", Name: "fixture-model", Context: 32768, MaxOutput: 4096}})
							model := c.Describe().ID + "/" + identity.Scope + "/fixture-model"
							if public {
								_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "shared-fixture-v1", Enabled: true, Sources: []config.ModelSource{{ID: model, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
								if err != nil {
									t.Fatal(err)
								}
								model = "shared-fixture-v1"
							}
							key := s.Store.AddKey("fixture").Key
							_ = s.Handler()
							markModelsFresh(s)
							body := map[string]any{"model": model, "stream": stream, "max_tokens": 64, "messages": []map[string]string{{"role": "user", "content": "fixture"}}}
							if path == "/v1/responses" {
								delete(body, "messages")
								delete(body, "max_tokens")
								body["input"] = "fixture"
								body["max_output_tokens"] = 64
							}
							w := responseRequest(s, key, path, string(mustJSON(body)))
							if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "API_FIXTURE_OK") {
								t.Fatal(w.Code, calls, w.Body.String())
							}
							logs := s.Store.Logs(1)
							if len(logs) != 1 || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 || logs[0].Usage.CachedTokens != 3 || logs[0].Provider != c.Describe().ID || logs[0].Model != c.Describe().ID+"/"+identity.Scope+"/fixture-model" || s.Store.PoolSnapshot()["mc"].InFlight != 0 {
								t.Fatal("accounting/source/lease mismatch", logs)
							}
							group, err := s.Store.SaveGroup(storage.AccountGroup{Name: "empty restricted group", AccountIDs: []string{}})
							if err != nil {
								t.Fatal(err)
							}
							if err = s.Store.SetKeyGroupAndSave(key, group.ID); err != nil {
								t.Fatal(err)
							}
							blocked := responseRequest(s, key, path, string(mustJSON(body)))
							if blocked.Code != 403 || calls != 1 {
								t.Fatal("group bypass", blocked.Code, calls)
							}

						})
					}
				}
			}

		})
	}
}

func antigravitySignedFixture(t *testing.T) (*Server, string, *int) {
	t.Helper()
	calls := 0

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1internal:streamGenerateContent" || !strings.Contains(string(body), `"project":"fixture-project"`) {
			t.Error("wrong native route")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			io.WriteString(w, "data: "+`{"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"n":9007199254740993}},"thoughtSignature":"SIGNED_TOOL"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":4,"thoughtsTokenCount":3,"cachedContentTokenCount":5}}}`+"\n\n")
		} else {
			for _, v := range []string{"SIGNED_TOOL", "TOOL_RESULT", "9007199254740993", "functionResponse"} {
				if !strings.Contains(string(body), v) {
					t.Error("lost native history", v)
				}
			}
			io.WriteString(w, "data: "+`{"response":{"candidates":[{"content":{"parts":[{"text":"GEMINI_TOOL_OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":4,"thoughtsTokenCount":3,"cachedContentTokenCount":5}}}`+"\n\n")
		}
	}))
	t.Cleanup(up.Close)
	s := newAdminTestServer(t)
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := antigravity.New()
	c.BaseURL = up.URL
	s.Providers = provider.NewRegistry()
	if err := s.Providers.Register(c); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(fmt.Sprintf(`{"access_token":"fixture-key","refresh_token":"fixture-refresh","subject":"fixture-user","project_id":"fixture-project","scope":"global","expires_at":%d}`, time.Now().Unix()+3600))
	_, id, _ := c.Normalize(raw)
	s.Store.UpsertAccount(&accounts.Account{ID: "gemini-fixture", Provider: "antigravity", Scope: "global", ExternalID: id.ID, Credentials: raw, Enabled: true})
	s.Reg.ReplaceProvider("antigravity", "global", []provider.Model{{ID: "gemini-3-flash-preview", ImageInput: "supported", ToolCalling: "supported"}})
	_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "gemini-3-flash-preview", Enabled: true, Sources: []config.ModelSource{{ID: "antigravity/global/gemini-3-flash-preview", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	key := s.Store.AddKey("fixture").Key
	_ = s.Handler()
	markModelsFresh(s)

	return s, key, &calls
}

func TestAntigravitySignedToolsAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", path, stream), func(t *testing.T) {
				s, key, count := antigravitySignedFixture(t)
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
					var blocks []struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					}
					json.Unmarshal(history, &blocks)
					toolID := ""
					for _, b := range blocks {
						if b.Type == "tool_use" {
							toolID = b.ID
						}
					}
					if toolID == "" {
						t.Fatal("missing tool ID")
					}
					if !strings.Contains(string(history), "SIGNED_TOOL") {
						t.Fatal("lost tool metadata", string(history))
					}
					next = fmt.Sprintf(`{"model":"gemini-3-flash-preview","max_tokens":128,"stream":%v,"messages":[{"role":"user","content":"lookup"},{"role":"assistant","content":%s},{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":"TOOL_RESULT"}]}]}`, stream, history, toolID)
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
					toolID := ""
					for _, raw := range output {
						var item struct {
							Type   string `json:"type"`
							CallID string `json:"call_id"`
						}
						json.Unmarshal(raw, &item)
						if item.Type == "function_call" {
							toolID = item.CallID
						}
					}
					if toolID == "" {
						t.Fatal("missing call ID")
					}
					input := []json.RawMessage{json.RawMessage(`{"role":"user","content":"lookup"}`)}
					input = append(input, output...)
					input = append(input, json.RawMessage(fmt.Sprintf(`{"type":"function_call_output","call_id":%q,"output":"TOOL_RESULT"}`, toolID)))
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
