package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/chatgpt"
	"codearts2api/internal/claude"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func subscriptionServer(t *testing.T, handler http.HandlerFunc) (*Server, string, *chatgpt.Client, *claude.Client) {
	t.Helper()
	st, e := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	st.UpdateSettings(func(s *storage.Settings) {
		s.AdminPassword = "admin-fixture"
		s.RateLimitEnabled = false
		s.MaxRetries = 1
	})
	s := newConfiguredServer(st)
	up := httptest.NewServer(handler)
	t.Cleanup(up.Close)
	g, a := chatgpt.New(), claude.New()
	g.API = up.URL + "/openai/v1"
	g.Backend = up.URL + "/codex"
	g.Token = up.URL + "/openai/token"
	a.API = up.URL
	a.Token = up.URL + "/claude/token"
	s.Providers = provider.NewRegistry()
	_ = s.Providers.Register(g)
	_ = s.Providers.Register(a)
	return s, st.AddKey("fixture").Key, g, a
}
func addSubscription(t *testing.T, s *Server, p provider.Adapter, key bool, expiry time.Time) {
	t.Helper()
	name := p.Describe().ID
	body := map[string]any{"expires_at": expiry.Unix(), "refresh_token": "refresh-fixture"}
	if name == provider.ChatGPT {
		body["access_token"] = "chatgpt-access-fixture"
		body["account_id"] = "account-fixture"
		body["user_id"] = "user-fixture"
	} else {
		body["access_token"] = "claude-access-fixture"
		body["account_uuid"] = "uuid-fixture"
	}
	if key {
		body = map[string]any{"api_key": "sk-fixture"}
		if name == provider.Claude {
			body["api_key"] = "sk-ant-fixture"
		}
	}
	raw, id, e := p.Normalize(mustJSON(body))
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Store.UpsertAccountAndSave(&accounts.Account{ID: name + "-fixture", Provider: name, Scope: "global", ExternalID: id.ID, Name: name, Credentials: raw, Enabled: true}, false)
	if e != nil {
		t.Fatal(e)
	}
	s.Reg.ReplaceProvider(name, "global", []provider.Model{{ID: "actual-model", StreamOnly: true}})
}
func subscriptionEvent(w io.Writer, typ string, v any) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, mustJSON(v))
}
func subscriptionComplete(w http.ResponseWriter, name string, tools bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	if name == provider.ChatGPT {
		if tools {
			subscriptionEvent(w, "response.output_item.added", map[string]any{"item": map[string]string{"type": "function_call", "id": "item1", "call_id": "call_fixture", "name": "lookup"}})
			subscriptionEvent(w, "response.function_call_arguments.delta", map[string]string{"item_id": "item1", "delta": "{\"id\":9007199254740993}"})
			subscriptionEvent(w, "response.function_call_arguments.done", map[string]string{"item_id": "item1", "arguments": "{\"id\":9007199254740993}"})
		} else {
			subscriptionEvent(w, "response.output_text.delta", map[string]string{"delta": "NATIVE_COMPLETE"})
		}
		subscriptionEvent(w, "response.completed", map[string]any{"response": map[string]any{"status": "completed", "usage": map[string]any{"input_tokens": 12, "output_tokens": 4, "input_tokens_details": map[string]int{"cached_tokens": 3}}}})
		return
	}
	subscriptionEvent(w, "message_start", map[string]any{"message": map[string]any{"id": "msg_fixture", "role": "assistant", "usage": map[string]int{"input_tokens": 7, "output_tokens": 0, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 2}}})
	block := map[string]any{"type": "text", "text": ""}
	if tools {
		block = map[string]any{"type": "tool_use", "id": "call_fixture", "name": "lookup", "input": map[string]any{}}
	}
	subscriptionEvent(w, "content_block_start", map[string]any{"index": 0, "content_block": block})
	d := map[string]string{"type": "text_delta", "text": "NATIVE_COMPLETE"}
	reason := "end_turn"
	if tools {
		d = map[string]string{"type": "input_json_delta", "partial_json": "{\"id\":9007199254740993}"}
		reason = "tool_use"
	}
	subscriptionEvent(w, "content_block_delta", map[string]any{"index": 0, "delta": d})
	subscriptionEvent(w, "content_block_stop", map[string]int{"index": 0})
	subscriptionEvent(w, "message_delta", map[string]any{"delta": map[string]string{"stop_reason": reason}, "usage": map[string]int{"output_tokens": 4}})
	subscriptionEvent(w, "message_stop", map[string]string{})
}
func TestSubscriptionsThreeProtocolsBothAuthModesAndAccounting(t *testing.T) {
	for _, name := range []string{provider.ChatGPT, provider.Claude} {
		for _, keyAuth := range []bool{false, true} {
			for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
				for _, stream := range []bool{false, true} {
					for _, tools := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/key=%v/%s/stream=%v/tools=%v", name, keyAuth, path, stream, tools), func(t *testing.T) {
							calls := 0
							s, key, g, a := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
								calls++
								wantPath := "/codex/responses"
								if name == provider.ChatGPT && keyAuth {
									wantPath = "/openai/v1/responses"
								}
								if name == provider.Claude {
									wantPath = "/v1/messages"
								}
								if r.URL.Path != wantPath {
									t.Error("wrong native path", r.URL.Path)
								}
								if name == provider.ChatGPT {
									want := "Bearer chatgpt-access-fixture"
									if keyAuth {
										want = "Bearer sk-fixture"
									}
									if r.Header.Get("Authorization") != want {
										t.Error("wrong OpenAI credential")
									}
									if !keyAuth && r.Header.Get("ChatGPT-Account-Id") != "account-fixture" {
										t.Error("workspace missing")
									}
								} else {
									if keyAuth {
										if r.Header.Get("X-Api-Key") != "sk-ant-fixture" || r.Header.Get("Authorization") != "" {
											t.Error("wrong Anthropic key")
										}
									} else if r.Header.Get("Authorization") != "Bearer claude-access-fixture" || r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
										t.Error("wrong Claude OAuth headers")
									}
								}
								var in map[string]json.RawMessage
								_ = json.NewDecoder(r.Body).Decode(&in)
								var m string
								_ = json.Unmarshal(in["model"], &m)
								if m != "actual-model" {
									t.Error("namespace leaked upstream", m)
								}
								if string(in["stream"]) != "true" {
									t.Error("upstream not streaming")
								}
								if !strings.Contains(string(mustJSON(in)), "CONTEXT_FIXTURE") {
									t.Error("question context lost")
								}
								subscriptionComplete(w, name, tools)
							})
							var p provider.Adapter = g
							if name == provider.Claude {
								p = a
							}
							addSubscription(t, s, p, keyAuth, time.Now().Add(time.Hour))
							s.Store.UpdateSettings(func(s *storage.Settings) { s.AutoRefresh = false })
							body := map[string]any{"model": name + "/global/actual-model", "messages": []map[string]string{{"role": "user", "content": "CONTEXT_FIXTURE"}}, "stream": stream, "max_tokens": 64}
							if path == "/v1/responses" {
								delete(body, "messages")
								body["input"] = "CONTEXT_FIXTURE"
							}
							w := responseRequest(s, key, path, string(mustJSON(body)))
							want := "NATIVE_COMPLETE"
							if tools {
								want = "9007199254740993"
							}
							if w.Code != 200 || !strings.Contains(w.Body.String(), want) || calls != 1 {
								t.Fatal(w.Code, w.Body.String(), calls)
							}
							log := s.Store.Logs(1)[0]
							if log.Provider != name || log.Scope != "global" || log.Usage.InputTokens != 12 || log.Usage.OutputTokens != 4 || log.Usage.CachedTokens != 3 {
								t.Fatalf("wrong accounting: %+v", log)
							}
							if s.Store.PoolSnapshot()[name+"-fixture"].InFlight != 0 {
								t.Fatal("lease leaked")
							}
						})
					}
				}
			}
		}
	}
}
func TestSubscriptionOAuthPreviewExplicitSaveAndAdminBoundary(t *testing.T) {
	for _, name := range []string{provider.ChatGPT, provider.Claude} {
		t.Run(name, func(t *testing.T) {
			exchanges := 0
			s, key, _, _ := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
				exchanges++
				var in map[string]string
				_ = json.NewDecoder(r.Body).Decode(&in)
				if in["grant_type"] != "authorization_code" || in["code_verifier"] == "" || in["code"] != "fixture" {
					t.Error("invalid PKCE exchange")
				}
				token := "header." + base64.RawURLEncoding.EncodeToString(mustJSON(map[string]any{"sub": "user-fixture", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "account-fixture", "chatgpt_user_id": "user-fixture"}})) + ".fixture"
				fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"refresh-fixture","expires_in":3600,"account":{"uuid":"uuid-fixture","email_address":"fixture@example.invalid"}}`, token)
			})
			start := `{"action":"start","provider":"` + name + `","scope":"global"}`
			if w := responseRequest(s, key, "/admin/api/provider-login", start); w.Code != 401 {
				t.Fatal("API key granted admin")
			}
			w := providerAdmin(s, start)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var begin struct {
				Session string `json:"session_id"`
				URL     string `json:"url"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &begin)
			u, _ := url.Parse(begin.URL)
			state := u.Query().Get("state")
			cb := "fixture#" + state
			if name == provider.ChatGPT {
				cb = "http://localhost:1455/auth/callback?code=fixture&state=" + state
			}
			check := string(mustJSON(map[string]string{"action": "check", "session_id": begin.Session, "input": cb}))
			w = providerAdmin(s, check)
			if w.Code != 200 || strings.Contains(w.Body.String(), "refresh-fixture") || len(s.Store.Accounts()) != 0 {
				t.Fatal("preview leaked or persisted", w.Code, w.Body.String())
			}
			var preview struct {
				ID string `json:"preview_id"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &preview)
			w = providerAdmin(s, check)
			if w.Code != 200 || exchanges != 1 {
				t.Fatal("one-time code replayed", w.Body.String(), exchanges)
			}
			w = providerAdmin(s, string(mustJSON(map[string]string{"action": "complete", "session_id": begin.Session, "preview_id": preview.ID})))
			if w.Code != 200 || len(s.Store.Accounts()) != 1 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
func TestSubscriptionRefreshCommitsBeforeInference(t *testing.T) {
	for _, name := range []string{provider.ChatGPT, provider.Claude} {
		t.Run(name, func(t *testing.T) {
			refreshes, inferences := 0, 0
			s, key, g, a := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/token") {
					refreshes++
					fmt.Fprint(w, `{"access_token":"rotated-fixture","refresh_token":"next-refresh","expires_in":3600}`)
					return
				}
				inferences++
				if r.Header.Get("Authorization") != "Bearer rotated-fixture" {
					t.Error("old credential used")
				}
				subscriptionComplete(w, name, false)
			})
			var p provider.Adapter = g
			if name == provider.Claude {
				p = a
			}
			addSubscription(t, s, p, false, time.Now().Add(time.Minute))
			w := responseRequest(s, key, "/v1/chat/completions", `{"model":"`+name+`/global/actual-model","messages":[{"role":"user","content":"hi"}]}`)
			if w.Code != 200 || refreshes != 1 || inferences != 1 {
				t.Fatal(w.Code, w.Body.String(), refreshes, inferences)
			}
			reopened, e := storage.Open(s.Store.Path())
			if e != nil || !strings.Contains(string(reopened.Account(name+"-fixture").Credentials), "next-refresh") {
				t.Fatal("rotated credentials not persisted", e)
			}
		})
	}
}
func TestSubscriptionMappingPermissionsAndNativeThinking(t *testing.T) {
	gCalls, aCalls := 0, 0
	s, _, g, a := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/messages" {
			aCalls++
			var in map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&in)
			if len(in["thinking"]) > 0 && !strings.Contains(string(in["thinking"]), "adaptive") {
				t.Error("native thinking changed")
			}
			subscriptionComplete(w, provider.Claude, false)
		} else {
			gCalls++
			subscriptionComplete(w, provider.ChatGPT, false)
		}
	})
	addSubscription(t, s, g, true, time.Now())
	addSubscription(t, s, a, true, time.Now())
	ids := []string{"chatgpt/global/actual-model", "claude/global/actual-model"}
	group, e := s.Store.SaveGroup(storage.AccountGroup{Name: "shared", AccountIDs: []string{"chatgpt-fixture", "claude-fixture"}, AllowlistEnabled: true, ModelIDs: ids, ModelMappings: []storage.ModelMapping{{Name: "shared-1.0", Sources: ids}}})
	if e != nil {
		t.Fatal(e)
	}
	key, e := s.Store.AddKeyInGroupAndSave("mapped", group.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = s.Handler()
	markModelsFresh(s)
	models := modelIDsFromResponse(t, scopedModelRequest(s, key.Key))
	if len(models) != 1 || models[0] != "shared-1.0" {
		t.Fatal("raw models exposed", models)
	}
	for _, name := range []string{provider.ChatGPT, provider.Claude} {
		onlyMappedPlatform(s, name)
		w := responseRequest(s, key.Key, "/v1/chat/completions", `{"model":"shared-1.0","messages":[{"role":"user","content":"hi"}]}`)
		if w.Code != 200 || s.Store.Logs(1)[0].Provider != name {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	s.Store.SetAccountPaused("chatgpt-fixture", false)
	s.Store.SetAccountPaused("claude-fixture", false)
	w := responseRequest(s, key.Key, "/v1/messages", `{"model":"shared-1.0","messages":[{"role":"user","content":"hi"}],"max_tokens":2048,"thinking":{"type":"adaptive"}}`)
	if w.Code != 200 || s.Store.Logs(1)[0].Provider != provider.Claude {
		t.Fatal("native thinking scheduled to incompatible target", w.Code, w.Body.String())
	}
	group.AllowlistEnabled = true
	group.ModelIDs = []string{ids[0]}
	group.ModelMappings = nil
	_, e = s.Store.SaveGroup(group)
	if e != nil {
		t.Fatal(e)
	}
	before := gCalls + aCalls
	w = responseRequest(s, key.Key, "/v1/chat/completions", `{"model":"claude/global/actual-model","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 403 || gCalls+aCalls != before {
		t.Fatal("raw model bypassed allowlist", w.Code)
	}
}

func TestClaudeSignedThinkingAndCacheUsageThroughMessages(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			calls := 0
			s, key, _, a := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				if !strings.Contains(string(body["thinking"]), "adaptive") {
					t.Error("thinking lost")
				}
				if calls > 1 {
					b := string(body["messages"])
					for _, want := range []string{"SIGNED", "OPAQUE", "9007199254740993", "TOOL_RESULT"} {
						if !strings.Contains(b, want) {
							t.Error("history lost", want, b)
						}
					}
					subscriptionComplete(w, provider.Claude, false)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				subscriptionEvent(w, "message_start", map[string]any{"message": map[string]any{"id": "msg_signed", "role": "assistant", "usage": map[string]int{"input_tokens": 7, "output_tokens": 0, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 2}}})
				subscriptionEvent(w, "content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "thinking", "thinking": ""}})
				subscriptionEvent(w, "content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "thinking_delta", "thinking": "THOUGHT"}})
				subscriptionEvent(w, "content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "signature_delta", "signature": "SIGNED"}})
				subscriptionEvent(w, "content_block_stop", map[string]int{"index": 0})
				subscriptionEvent(w, "content_block_start", map[string]any{"index": 1, "content_block": map[string]string{"type": "redacted_thinking", "data": "OPAQUE"}})
				subscriptionEvent(w, "content_block_stop", map[string]int{"index": 1})
				subscriptionEvent(w, "content_block_start", map[string]any{"index": 2, "content_block": map[string]any{"type": "tool_use", "id": "call_fixture", "name": "lookup", "input": map[string]any{}}})
				subscriptionEvent(w, "content_block_delta", map[string]any{"index": 2, "delta": map[string]string{"type": "input_json_delta", "partial_json": "{\"id\":9007199254740993}"}})
				subscriptionEvent(w, "content_block_stop", map[string]int{"index": 2})
				subscriptionEvent(w, "message_delta", map[string]any{"delta": map[string]string{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": 4}})
				subscriptionEvent(w, "message_stop", map[string]string{})
			})
			addSubscription(t, s, a, true, time.Now())
			body := map[string]any{"model": "claude/global/actual-model", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "max_tokens": 2048, "thinking": map[string]string{"type": "adaptive"}, "stream": stream}
			w := responseRequest(s, key, "/v1/messages", string(mustJSON(body)))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, want := range []string{"SIGNED", "OPAQUE", "9007199254740993", `"cache_creation_input_tokens":2`, `"input_tokens":7`} {
				if !strings.Contains(w.Body.String(), want) {
					t.Fatal("native response lost field", want, w.Body.String())
				}
			}
			if !stream {
				var reply struct {
					Content json.RawMessage `json:"content"`
				}
				_ = json.Unmarshal(w.Body.Bytes(), &reply)
				body["messages"] = []any{map[string]any{"role": "user", "content": "hi"}, map[string]any{"role": "assistant", "content": reply.Content}, map[string]any{"role": "user", "content": []any{map[string]string{"type": "tool_result", "tool_use_id": "call_fixture", "content": "TOOL_RESULT"}}}}
				w = responseRequest(s, key, "/v1/messages", string(mustJSON(body)))
				if w.Code != 200 || calls != 2 {
					t.Fatal("signed tool follow-up failed", w.Code, w.Body.String())
				}
			}
		})
	}
}
func TestSubscriptionTruncatedStreamsFailAndRespectRetryBudget(t *testing.T) {
	for _, name := range []string{provider.ChatGPT, provider.Claude} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", name, stream), func(t *testing.T) {
				calls := 0
				s, key, g, a := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.Header().Set("Content-Type", "text/event-stream")
					if name == provider.ChatGPT {
						subscriptionEvent(w, "response.output_text.delta", map[string]string{"delta": "PARTIAL"})
					} else {
						subscriptionEvent(w, "message_start", map[string]any{"message": map[string]any{"role": "assistant", "usage": map[string]int{"input_tokens": 1, "output_tokens": 0}}})
						subscriptionEvent(w, "content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": "PARTIAL"}})
					}
				})
				var p provider.Adapter = g
				if name == provider.Claude {
					p = a
				}
				addSubscription(t, s, p, true, time.Now())
				c := map[string]string{"api_key": "sk-second-fixture"}
				if name == provider.Claude {
					c["api_key"] = "sk-ant-second-fixture"
				}
				raw, id, e := p.Normalize(mustJSON(c))
				if e != nil {
					t.Fatal(e)
				}
				_, e = s.Store.UpsertAccountAndSave(&accounts.Account{ID: "second", Provider: name, Scope: "global", ExternalID: id.ID, Credentials: raw, Enabled: true}, false)
				if e != nil {
					t.Fatal(e)
				}
				s.Store.UpdateSettings(func(st *storage.Settings) { st.MaxRetries = 2 })
				w := responseRequest(s, key, "/v1/chat/completions", string(mustJSON(map[string]any{"model": name + "/global/actual-model", "messages": []map[string]string{{"role": "user", "content": "hi"}}, "stream": stream})))
				if !stream && w.Code != 502 || stream && !strings.Contains(w.Body.String(), `"error"`) {
					t.Fatal("truncated stream succeeded", w.Code, w.Body.String())
				}
				wantCalls := 1
				if !stream {
					wantCalls = 2
				} // Buffered failures can retry within the existing budget; visible SSE cannot.
				if calls != wantCalls || s.Store.Logs(1)[0].Status == 200 {
					t.Fatal("retry budget or output boundary violated", calls)
				}
				for _, v := range s.Store.PoolSnapshot() {
					if v.InFlight != 0 {
						t.Fatal("lease leaked")
					}
				}
			})
		}
	}
}
