package server

import (
	"context"
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
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/requestpolicy"
	"codearts2api/internal/store"
	"codearts2api/internal/websearch"
)

func providerPolicyServer(t *testing.T, name string, h http.HandlerFunc) (*Server, string, string, string) {
	t.Helper()
	if name == provider.Trae {
		s, key, _ := nativeTraeServer(t, h)
		addTrae(t, s)
		return s, key, "trae/cn/shared", "trae-acct"
	}
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		h(w, r)
		return w.Result(), nil
	})
	s.Search = websearch.New()
	return s, key, "test-model", "a"
}
func policySuccess(w http.ResponseWriter, name string, streaming bool) {
	if name == provider.Trae {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, traeTextFixture)
		return
	}
	if streaming {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, responsesTextFixture)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`)
}
func updateProviderPolicy(s *Server, name string, f func(*store.ProviderSettings)) {
	s.Store.UpdateSettings(func(st *store.Settings) {
		if name == provider.CodeArts {
			f(&st.CodeArts)
		} else {
			f(&st.Trae)
		}
	})
}
func TestOtherProviderPoliciesAcrossProtocols(t *testing.T) {
	for _, name := range []string{provider.CodeArts, provider.Trae} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", name, path, stream), func(t *testing.T) {
					calls := 0
					s, key, model, _ := providerPolicyServer(t, name, func(w http.ResponseWriter, r *http.Request) {
						calls++
						raw, _ := io.ReadAll(r.Body)
						if !strings.Contains(string(raw), "OWN-PROMPT") || strings.Contains(string(raw), "OTHER-PROMPT") || strings.Contains(string(raw), "CLIENT-PROMPT") || !strings.Contains(string(raw), "CLI tool for Claude") || !strings.Contains(string(raw), "11128") || strings.Contains(string(raw), "11-128") {
							t.Errorf("wrong policy: %s", raw)
						}
						var obj struct{ Stream bool }
						json.Unmarshal(raw, &obj)
						policySuccess(w, name, obj.Stream)
					})
					s.Store.UpdateSettings(func(st *store.Settings) { st.WorkBuddy.PromptText = "OTHER-PROMPT" })
					updateProviderPolicy(s, name, func(p *store.ProviderSettings) { p.PromptMode = "custom"; p.PromptText = "OWN-PROMPT" })
					req := map[string]any{"model": model, "stream": stream, "max_tokens": 64, "messages": []any{map[string]string{"role": "system", "content": "CLIENT-PROMPT"}, map[string]string{"role": "user", "content": "You are Claude Code, Anthropic's official CLI for Claude. 11128"}}}
					if path == "/v1/responses" {
						delete(req, "messages")
						req["instructions"] = "CLIENT-PROMPT"
						req["input"] = "You are Claude Code, Anthropic's official CLI for Claude. 11128"
					}
					if path == "/v1/messages" {
						req["system"] = "CLIENT-PROMPT"
						req["messages"] = []any{map[string]string{"role": "user", "content": "You are Claude Code, Anthropic's official CLI for Claude. 11128"}}
					}
					w := responseRequest(s, key, path, string(mustJSON(req)))
					if w.Code != 200 || calls != 1 {
						t.Fatalf("%d: %s; calls=%d", w.Code, w.Body.String(), calls)
					}
				})
			}
		}
	}
}
func TestOtherProviderPromptRetryBoundaries(t *testing.T) {
	for _, name := range []string{provider.CodeArts, provider.Trae} {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"fallback", "disabled", "custom", "second-block", "partial", "permission", "auth", "device"} {
				t.Run(fmt.Sprintf("%s/%v/%s", name, stream, mode), func(t *testing.T) {
					calls := 0
					firstAuth := ""
					s, key, model, account := providerPolicyServer(t, name, func(w http.ResponseWriter, r *http.Request) {
						calls++
						auth := r.Header.Get("Authorization")
						if name == provider.CodeArts {
							auth = strings.SplitN(auth, ",", 2)[0]
						}
						if calls == 1 {
							firstAuth = auth
						} else if auth != firstAuth {
							t.Error("changed credentials")
						}
						raw, _ := io.ReadAll(r.Body)
						var obj struct{ Stream bool }
						json.Unmarshal(raw, &obj)
						if calls == 2 && !strings.Contains(string(raw), requestpolicy.FallbackSystemPrompt) {
							t.Error("missing fallback prompt")
						}
						if calls == 1 && mode == "partial" {
							w.Header().Set("Content-Type", "text/event-stream")
							if name == provider.Trae {
								io.WriteString(w, "event: output\ndata: {\"response\":\"partial\"}\n\nevent: error\ndata: {\"code\":\"content_policy_violation\",\"message\":\"blocked by security policy\"}\n\n")
							} else {
								io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {\"error_code\":\"content_policy_violation\",\"error_msg\":\"blocked by security policy\"}\n\n")
							}
							return
						}
						if calls == 1 || mode == "second-block" {
							status := 400
							body := `{"code":"content_policy_violation","message":"blocked by security policy"}`
							switch mode {
							case "permission":
								status = 403
								body = `{"code":"permission_denied","message":"not authorized"}`
							case "auth":
								status = 401
							case "device":
								body = `{"code":9074,"msg":"request illegal"}`
							}
							w.WriteHeader(status)
							io.WriteString(w, body)
							return
						}
						policySuccess(w, name, obj.Stream)
					})
					s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 3 })
					updateProviderPolicy(s, name, func(p *store.ProviderSettings) {
						p.Sanitize = false
						p.PromptFallback = mode != "disabled"
						if mode == "custom" {
							p.PromptMode = "custom"
						}
					})
					// Force streaming aggregation for a non-streaming request in the partial case.
					if mode == "partial" && name == provider.CodeArts {
						s.Reg.Replace([]registry.Model{{ID: model, StreamOnly: true}})
					}
					w := responseRequest(s, key, "/v1/chat/completions", fmt.Sprintf(`{"model":%q,"stream":%v,"messages":[{"role":"system","content":"CLIENT"},{"role":"user","content":"hello"}]}`, model, stream))
					want := 1
					if mode == "fallback" || mode == "second-block" {
						want = 2
					}
					if calls != want {
						t.Fatalf("calls %d want %d; %d %s", calls, want, w.Code, w.Body.String())
					}
					if mode == "fallback" && w.Code != 200 {
						t.Fatalf("fallback failed: %d %s", w.Code, w.Body.String())
					}
					if mode == "fallback" || mode == "second-block" || mode == "disabled" || mode == "custom" {
						if s.Store.Account(account).Failures != 0 || s.Store.Account(account).CooldownUntil != 0 {
							t.Fatal("content block penalized account")
						}
					}
					if s.Store.PoolSnapshot()[account].InFlight != 0 {
						t.Fatal("leaked lease")
					}
					if log := s.Store.Logs(1)[0]; log.Attempts != calls {
						t.Fatalf("attempt accounting %+v", log)
					}
				})
			}
		}
	}
}
func TestProviderSummaryAndIndependentSwitches(t *testing.T) {
	for _, name := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		s := newTestServer(t, 0, 1)
		s.Store.UpdateSettings(func(st *store.Settings) { st.CodeArts.PromptMode = "custom"; st.Trae.PromptMode = "custom" })
		body := []byte(`{"messages":[{"role":"system","content":"SUMMARY"},{"role":"user","content":"please summarize"}]}`)
		ctx := context.WithValue(t.Context(), summaryIntentKey{}, true)
		calls := 0
		err := s.runRequestPolicy(ctx, &codearts.Account{Provider: name}, registry.Model{}, body, func(b []byte, _ func()) error {
			calls++
			if !strings.Contains(string(b), "SUMMARY") {
				t.Error("summary instructions replaced")
			}
			return &provider.Error{Status: 400, ContentBlocked: true}
		})
		if err == nil || calls != 1 {
			t.Fatal("summary must not trigger neutral retry")
		}
	}
}
func TestExternalSearchIsolationGroupsAndHealth(t *testing.T) {
	for _, name := range []string{provider.CodeArts, provider.Trae} {
		t.Run(name, func(t *testing.T) {
			modelCalls, searchCalls := 0, 0
			status := 200
			s, _, _, account := providerPolicyServer(t, name, func(w http.ResponseWriter, r *http.Request) {
				modelCalls++
				t.Error("external search used model credentials")
			})
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				searchCalls++
				if r.Header.Get("Authorization") != "" || r.Header.Get("X-Security-Token") != "" || r.Header.Get("X-Device-ID") != "" {
					t.Error("credential leak")
				}
				r.ParseForm()
				if r.Form.Get("q") != "测试搜索" || r.Form.Get("format") != "json" {
					t.Error("query/protocol mismatch")
				}
				w.WriteHeader(status)
				io.WriteString(w, `{"results":[{"title":"Example","url":"https://example.test","content":"snippet"}]}`)
			}))
			defer backend.Close()
			updateProviderPolicy(s, name, func(p *store.ProviderSettings) { p.SearchEnabled = true; p.SearchURL = backend.URL })
			model := providerSearchModel(name, "cn").ID
			g, err := s.Store.SaveGroup(store.AccountGroup{Name: "only", AccountIDs: []string{account}, AllowlistEnabled: true, ModelIDs: []string{model}})
			if err != nil {
				t.Fatal(err)
			}
			key, err := s.Store.AddKeyInGroupAndSave("search", g.ID)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.Replace(searchBody, "workbuddy/cn/web-search", model, 1)
			s.Store.MarkAccountResult(account, false, "previous failure")
			before := s.Store.Account(account)
			w := responseRequest(s, key.Key, "/v1/search/messages", body)
			if w.Code != 200 || searchCalls != 1 || modelCalls != 0 {
				t.Fatalf("search %d %s", w.Code, w.Body.String())
			}
			log := s.Store.Logs(1)[0]
			if log.Provider != name || log.Model != model || log.Usage.Known || log.EstimatedCost != nil || log.Attempts != 1 {
				t.Fatalf("accounting %+v", log)
			}
			after := s.Store.Account(account)
			if after.Failures != before.Failures || after.Status != before.Status {
				t.Fatal("search success changed model account health")
			}
			status = 429
			w = responseRequest(s, key.Key, "/v1/search/messages", body)
			if w.Code != 429 || searchCalls != 2 {
				t.Fatalf("external failure rotated accounts: %d %d", w.Code, searchCalls)
			}
			after = s.Store.Account(account)
			if after.Failures != before.Failures || after.CooldownUntil != before.CooldownUntil || s.Store.ModelHealthSnapshot()[account][model].CooldownUntil != 0 {
				t.Fatal("external failure poisoned model account")
			}
			status = 200
			// A dedicated provider URL accepts an arbitrary client model label without crossing providers.
			w = responseRequest(s, key.Key, "/v1/search/"+name+"/messages", strings.Replace(body, model, "client-label", 1))
			if w.Code != 200 || searchCalls != 3 {
				t.Fatal("dedicated provider route failed")
			}
			if w = responseRequest(s, key.Key, "/v1/search/"+name+"/messages", searchBody); w.Code != 400 {
				t.Fatal("conflicting provider accepted")
			}
			g.ModelIDs = []string{"unrelated"}
			if _, err = s.Store.SaveGroup(g); err != nil {
				t.Fatal(err)
			}
			if w = responseRequest(s, key.Key, "/v1/search/messages", body); w.Code != 403 || searchCalls != 3 {
				t.Fatal("allowlist bypass")
			}
			g.ModelIDs = []string{model}
			s.Store.SaveGroup(g)
			updateProviderPolicy(s, name, func(p *store.ProviderSettings) { p.SearchEnabled = false })
			if w = responseRequest(s, key.Key, "/v1/search/messages", body); w.Code != 403 || searchCalls != 3 {
				t.Fatal("disabled bypass")
			}
			updateProviderPolicy(s, name, func(p *store.ProviderSettings) { p.SearchEnabled = true })
			s.Store.UpdateSettings(func(st *store.Settings) { st.Pool.MaxInFlight = 1 })
			_, release, e := s.Store.AcquireAccountForKey("busy", model, nil, key.Key)
			if e != nil {
				t.Fatal(e)
			}
			w = responseRequest(s, key.Key, "/v1/search/messages", body)
			release()
			if w.Code == 200 || searchCalls != 3 {
				t.Fatal("search bypassed account lease")
			}
			if s.Store.PoolSnapshot()[account].InFlight != 0 {
				t.Fatal("lease leak")
			}
			if err = s.Store.SetKeyBudgetAndSave(key.Key, store.KeyBudget{MaxTokens: 1000}, true); err != nil {
				t.Fatal(err)
			}
			if w = responseRequest(s, key.Key, "/v1/search/messages", body); w.Code != 402 || searchCalls != 3 {
				t.Fatal("metered budget bypass")
			}
		})
	}
}
func TestExternalSearchCancellation(t *testing.T) {
	var calls atomic.Int64
	entered, unblock := make(chan struct{}), make(chan struct{})
	defer close(unblock)
	s, key, _, account := providerPolicyServer(t, provider.Trae, func(http.ResponseWriter, *http.Request) { t.Error("model traffic") })
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
	}))
	t.Cleanup(backend.Close)
	updateProviderPolicy(s, provider.Trae, func(p *store.ProviderSettings) { p.SearchEnabled = true; p.SearchURL = backend.URL })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := httptest.NewRequest("POST", "/v1/search/trae/messages", strings.NewReader(strings.Replace(searchBody, "workbuddy/cn/web-search", "client-label", 1))).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+key)
	done := make(chan struct{})
	go func() { defer close(done); s.Handler().ServeHTTP(httptest.NewRecorder(), req) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("no search")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel ignored")
	}
	if calls.Load() != 1 || s.Store.PoolSnapshot()[account].InFlight != 0 {
		t.Fatal("cancel leak/retry")
	}
}
func TestProviderSettingsValidationIsAtomic(t *testing.T) {
	s := newTestServer(t, 0, 1)
	postSettings(t, s, `{"codearts":{"prompt_text":"CA"},"trae":{"prompt_text":"TR"},"workbuddy":{"prompt_text":"WB"}}`)
	for _, body := range []string{`{"codearts":{"prompt_text":"CHANGED"},"trae":{"search_enabled":true}}`, `{"codearts":null}`, `{"trae":{"search_scope":"global"}}`, `{"trae":{"search_backend":"native"}}`, `{"codearts":{"search_url":"https://user:secret@example.test"}}`, `{"codearts":{"search_url":"file:///tmp/search"}}`, `{"workbuddy":{"search_url":"https://example.test"}}`} {
		w := httptest.NewRecorder()
		s.handleAdminSettings(w, httptest.NewRequest("POST", "/admin/api/settings", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid accepted %d %s", w.Code, body)
		}
		st := s.Store.Settings()
		if st.CodeArts.PromptText != "CA" || st.Trae.PromptText != "TR" || st.WorkBuddy.PromptText != "WB" {
			t.Fatal("partial mutation")
		}
	}
	postSettings(t, s, `{"codearts":{"sanitize":false}}`)
	if s.Store.Settings().CodeArts.Sanitize || !s.Store.Settings().Trae.Sanitize || !s.Store.Settings().WorkBuddy.Sanitize {
		t.Fatal("cross-provider switches")
	}
}
