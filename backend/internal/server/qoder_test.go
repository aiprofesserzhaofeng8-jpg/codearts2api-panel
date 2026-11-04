package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/qoder"
	"codearts2api/internal/storage"
	"codearts2api/internal/workbuddy"
)

func nativeQoderServer(t *testing.T, handler http.HandlerFunc) (*Server, string, *qoder.Client) {
	t.Helper()
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateSettings(func(c *storage.Settings) {
		c.AdminPassword = "admin-fixture"
		c.RateLimitEnabled = false
		c.MaxRetries = 1
	})
	s := newConfiguredServer(st)
	up := httptest.NewServer(handler)
	t.Cleanup(up.Close)
	c := qoder.New()
	for scope, region := range c.Regions {
		region.OpenAPI, region.Inference = up.URL, up.URL
		c.Regions[scope] = region
	}
	s.Providers = provider.NewRegistry()
	if err := s.Providers.Register(c); err != nil {
		t.Fatal(err)
	}
	return s, st.AddKey("qoder fixture").Key, c
}

func addQoder(t *testing.T, s *Server, scope string) {
	t.Helper()
	raw, _ := json.Marshal(qoder.Credentials{AccessToken: "dt-fixture", RefreshToken: "drt-fixture", UID: "qoder-user", MachineID: "qoder-machine", Scope: scope, ExpiresAt: time.Now().Add(time.Hour).Unix(), UserType: "personal_standard"})
	_, err := s.Store.UpsertAccountAndSave(&accounts.Account{ID: "qoder-" + scope, Name: "Qoder fixture", Provider: provider.Qoder, Scope: scope, ExternalID: "qoder-user", Credentials: raw, Enabled: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	s.Reg.ReplaceProvider(provider.Qoder, scope, []provider.Model{{ID: "qmodel", StreamOnly: true}})
}

func qoderFrame(inner string) string {
	b, _ := json.Marshal(map[string]any{"statusCodeValue": 200, "body": inner})
	return "data: " + string(b) + "\n\n"
}

const qoderText = `{"id":"qoder-fixture","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"QODER_COMPLETE","reasoning_content":"thought"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":2}}}`

func TestQoderThreeProtocolsRegionsAndAccounting(t *testing.T) {
	for _, scope := range []string{"cn", "global"} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", scope, path, stream), func(t *testing.T) {
					calls := 0
					s, key, _ := nativeQoderServer(t, func(w http.ResponseWriter, r *http.Request) {
						calls++
						if r.URL.Path != strings.SplitN(qoder.ChatPath, "?", 2)[0] || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer COSY.") || r.Header.Get("X-Model-Key") != "qmodel" {
							t.Error("incorrect native route, authorization or model")
						}
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, qoderFrame(qoderText)+qoderFrame("[DONE]")+"event: finish\ndata: {}\n\n")
					})
					addQoder(t, s, scope)
					body := map[string]any{"model": "qoder/" + scope + "/qmodel", "messages": []map[string]string{{"role": "user", "content": "hi"}}, "stream": stream, "max_tokens": 64}
					if path == "/v1/responses" {
						delete(body, "messages")
						body["input"] = "hi"
					}
					w := responseRequest(s, key, path, string(mustJSON(body)))
					if w.Code != 200 || !strings.Contains(w.Body.String(), "QODER_COMPLETE") || calls != 1 {
						t.Fatalf("completion failed: %d %s (calls=%d)", w.Code, w.Body.String(), calls)
					}
					log := s.Store.Logs(1)[0]
					if log.Provider != provider.Qoder || log.Scope != scope || log.Model != body["model"] || log.Usage.InputTokens != 12 || log.Usage.OutputTokens != 4 || log.Usage.CachedTokens != 3 || log.Usage.ReasoningTokens != 2 {
						t.Fatalf("wrong accounting: %+v", log)
					}
					if s.Store.PoolSnapshot()["qoder-"+scope].InFlight != 0 {
						t.Fatal("lease leaked")
					}
				})
			}
		}
	}
}

func TestQoderSharesUnifiedMappingWithoutBypassingGroup(t *testing.T) {
	qoderCalls, wbCalls := 0, 0
	s, _, c := nativeQoderServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if r.Header.Get("X-Model-Key") == "qmodel" {
			qoderCalls++
			io.WriteString(w, qoderFrame(qoderText)+"event: finish\ndata: {}\n\n")
		} else {
			wbCalls++
			io.WriteString(w, summaryFixture("WB_COMPLETE", "stop", 10, 4))
		}
	})
	addQoder(t, s, "cn")
	wb := workbuddy.New()
	wb.CNBase = c.Regions["cn"].Inference
	if err := s.Providers.Register(wb); err != nil {
		t.Fatal(err)
	}
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	g, err := s.Store.SaveGroup(storage.AccountGroup{Name: "fixture", AccountIDs: []string{"qoder-cn", "wb-cn"}, AllowlistEnabled: true, ModelIDs: []string{"qoder/cn/qmodel", "workbuddy/cn/shared"}, ModelMappings: []storage.ModelMapping{{Name: "fixture-1.0", Sources: []string{"qoder/cn/qmodel", "workbuddy/cn/shared"}}}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddKeyInGroupAndSave("mapped", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Handler()
	markModelsFresh(s)
	if ids := modelIDsFromResponse(t, scopedModelRequest(s, k.Key)); !slices.Equal(ids, []string{"fixture-1.0"}) {
		t.Fatal("raw model leaked into unified menu", ids)
	}
	for _, p := range []string{provider.Qoder, provider.WorkBuddy} {
		onlyMappedPlatform(s, p)
		w := responseRequest(s, k.Key, "/v1/chat/completions", `{"model":"fixture-1.0","messages":[{"role":"user","content":"hi"}]}`)
		if w.Code != 200 || s.Store.Logs(1)[0].Provider != p {
			t.Fatalf("mapping failed for %s: %d %s", p, w.Code, w.Body.String())
		}
	}
	onlyMappedPlatform(s, provider.Qoder)
	if w := responseRequest(s, k.Key, "/v1/chat/completions", `{"model":"qoder/cn/qmodel","messages":[{"role":"user","content":"hi"}]}`); w.Code != 200 {
		t.Fatal("authorized hidden raw model failed", w.Code, w.Body.String())
	}
	g.AccountIDs, g.ModelIDs = []string{"wb-cn"}, []string{"workbuddy/cn/shared"}
	g.ModelMappings[0].Sources = []string{"workbuddy/cn/shared"}
	if _, err := s.Store.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	if w := responseRequest(s, k.Key, "/v1/chat/completions", `{"model":"qoder/cn/qmodel","messages":[{"role":"user","content":"hi"}]}`); w.Code != 403 {
		t.Fatal("Qoder bypassed group", w.Code, w.Body.String())
	}
	if qoderCalls != 2 || wbCalls != 1 {
		t.Fatalf("unauthorized/unexpected upstream calls: Qoder=%d WB=%d", qoderCalls, wbCalls)
	}
}

func TestQoderLoginPreviewRequiresAdminAndExplicitSave(t *testing.T) {
	polls, profiles := 0, 0
	s, key, _ := nativeQoderServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/deviceToken/poll":
			polls++
			io.WriteString(w, `{"token":"dt-private-fixture","refresh_token":"drt-private-fixture","expires_in":3600000}`)
		case "/api/v1/userinfo":
			profiles++
			io.WriteString(w, `{"id":"qoder-user","name":"Qoder verified","user_type":"personal_standard"}`)
		default:
			t.Error("unexpected upstream request", r.URL.Path)
		}
	})
	w := responseRequest(s, key, "/admin/api/provider-login", `{"action":"start","provider":"qoder","scope":"cn"}`)
	if w.Code != 401 {
		t.Fatal("API key granted administrator access", w.Code)
	}
	w = providerAdmin(s, `{"action":"start","provider":"qoder","scope":"cn"}`)
	if w.Code != 200 || polls != 0 {
		t.Fatal("start failed or polled automatically", w.Code, w.Body.String())
	}
	var flow map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &flow)
	check := string(mustJSON(map[string]any{"action": "check", "session_id": flow["session_id"]}))
	w = providerAdmin(s, check)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ready":true`) || strings.Contains(w.Body.String(), "dt-private-fixture") || len(s.Store.Accounts()) != 0 {
		t.Fatal("preview leaked credentials or saved automatically", w.Code, w.Body.String())
	}
	var preview map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if w = providerAdmin(s, check); w.Code != 200 || polls != 1 || profiles != 1 {
		t.Fatal("preview unnecessarily exchanged token twice")
	}
	complete := string(mustJSON(map[string]any{"action": "complete", "session_id": flow["session_id"], "preview_id": preview["preview_id"]}))
	if w := providerAdmin(s, complete); w.Code != 200 || len(s.Store.Accounts()) != 1 || s.Store.Accounts()[0].ProviderID() != provider.Qoder {
		t.Fatal("explicit save failed", w.Code, w.Body.String())
	}
	if _, err := s.Store.RemoveAccountAndSave(s.Store.Accounts()[0].ID); err != nil {
		t.Fatal(err)
	}
	if w := providerAdmin(s, complete); w.Code != 410 {
		t.Fatal("old authorization resurrected deleted account", w.Code)
	}
}

func TestQoderPartialStreamIsNotReplayedOrMarkedSuccessful(t *testing.T) {
	calls := 0
	s, key, _ := nativeQoderServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, qoderFrame(`{"choices":[{"index":0,"delta":{"content":"partial-once"}}]}`))
	})
	addQoder(t, s, "cn")
	s.Store.UpdateSettings(func(c *storage.Settings) { c.MaxRetries = 3 })
	a := s.Store.Account("qoder-cn")
	a.ID, a.ExternalID = "qoder-second", "second"
	s.Store.UpsertAccount(a)
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"qoder/cn/qmodel","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if calls != 1 || strings.Count(w.Body.String(), "partial-once") != 1 {
		t.Fatal("partial response was replayed", calls, w.Body.String())
	}
	log := s.Store.Logs(1)[0]
	if log.Status < 400 || log.Usage.Known || !log.UsageIncomplete || s.Store.PoolSnapshot()["qoder-cn"].InFlight != 0 {
		t.Fatalf("truncation lost failure/accounting/lease: %+v", log)
	}
}

func TestQoderToolOutputAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", path, stream), func(t *testing.T) {
				s, key, _ := nativeQoderServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, qoderFrame(`{"id":"qoder-fixture","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_fixture","type":"function","function":{"name":"read","arguments":"{\"id\":9007199254740993}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`)+"event: finish\ndata: {}\n\n")
				})
				addQoder(t, s, "cn")
				body := map[string]any{"model": "qoder/cn/qmodel", "messages": []map[string]string{{"role": "user", "content": "read"}}, "stream": stream, "max_tokens": 64, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "parameters": map[string]any{"type": "object"}}}}}
				if path == "/v1/responses" {
					delete(body, "messages")
					body["input"] = "read"
					body["tools"] = []any{map[string]any{"type": "function", "name": "read", "parameters": map[string]any{"type": "object"}}}
				}
				if path == "/v1/messages" {
					body["tools"] = []any{map[string]any{"name": "read", "input_schema": map[string]any{"type": "object"}}}
				}
				w := responseRequest(s, key, path, string(mustJSON(body)))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "call_fixture") || !strings.Contains(w.Body.String(), "9007199254740993") {
					t.Fatalf("tool arguments lost in %s: %d %s", path, w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestQoderPromptSettingsPersistAndUnsupportedSearchFails(t *testing.T) {
	s, _, _ := nativeQoderServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("settings must not contact upstream")
	})
	update := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/admin/api/settings", strings.NewReader(body))
		r.AddCookie(adminTestCookie(s, "admin-fixture"))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := update(`{"common_prompt":{"prompt_mode":"custom","prompt_text":"COMMON","prompt_fallback":false,"sanitize":false},"qoder":{"prompt_overrides":{}}}`); w.Code != 200 {
		t.Fatal("common inheritance failed", w.Code, w.Body.String())
	}
	if p := s.Store.Settings().ProviderPolicy(provider.Qoder); p.PromptMode != "custom" || p.PromptText != "COMMON" || p.PromptFallback || p.Sanitize {
		t.Fatalf("wrong inherited policy: %+v", p)
	}
	if w := update(`{"qoder":{"prompt_overrides":{"prompt_text":"QODER ONLY"}}}`); w.Code != 200 {
		t.Fatal("override failed", w.Code, w.Body.String())
	}
	if w := update(`{"qoder":{"search_enabled":true,"search_url":"https://search.invalid"}}`); w.Code != 400 {
		t.Fatal("unsupported search accepted", w.Code, w.Body.String())
	}
	reopened, err := storage.Open(s.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	p := reopened.Settings().ProviderPolicy(provider.Qoder)
	if p.PromptMode != "custom" || p.PromptText != "QODER ONLY" || p.SearchEnabled {
		t.Fatalf("saved Qoder settings lost or invalid update persisted: %+v", p)
	}
}
