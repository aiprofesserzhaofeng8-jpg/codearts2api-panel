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

	"codearts2api/internal/requestpolicy"
	"codearts2api/internal/store"
	"codearts2api/internal/workbuddy"
)

const searchBody = `{"model":"workbuddy/cn/web-search","tools":[{"type":"web_search_20250305","name":"web_search"}],"messages":[{"role":"user","content":"old question"},{"role":"user","content":[{"type":"text","text":"Perform a web search for the query: 测试搜索"}]},{"role":"assistant","content":"do not search this reply"}]}`
const searchFixture = `{"results":[{"title":"Example","url":"https://example.test","snippet":"example snippet"}]}`

func TestSearchProtocolAuthBudgetAndUsage(t *testing.T) {
	calls := 0
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/agenttool/v1/search" {
			t.Error("search routed to chat")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "测试搜索" {
			t.Error("wrong query")
		}
		io.WriteString(w, searchFixture)
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	for _, bad := range []string{"", "invalid"} {
		w := responseRequest(s, bad, "/v1/search/messages", searchBody)
		if w.Code != 401 || !strings.Contains(w.Body.String(), `"type":"error"`) {
			t.Fatalf("auth %d %s", w.Code, w.Body.String())
		}
	}
	if calls != 0 {
		t.Fatal("unauthorized upstream call")
	}
	if err := s.Store.SetKeyBudgetAndSave(key, store.KeyBudget{MaxRequests: 1}, false); err != nil {
		t.Fatal(err)
	}
	w := responseRequest(s, key, "/v1/search/messages", searchBody)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "web_search_tool_result") || !strings.Contains(w.Body.String(), "web_search_result_location") || !strings.Contains(w.Body.String(), "example snippet") {
		t.Fatalf("response %d %s", w.Code, w.Body.String())
	}
	log := s.Store.Logs(1)[0]
	if log.Provider != "workbuddy" || log.Protocol != "search" || log.RequestKind != "search" || log.Usage.Known || log.EstimatedCost != nil || log.Attempts != 1 {
		t.Fatalf("search accounting %+v", log)
	}
	if s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
		t.Fatal("search leaked lease")
	}
	w = responseRequest(s, key, "/v1/search/messages", searchBody)
	if w.Code != 402 || calls != 1 {
		t.Fatal("request budget bypassed")
	}
}
func TestSearchValidationAndDisabledSwitch(t *testing.T) {
	calls := 0
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	for _, body := range []string{`{}`, `null`, `{"tools":[{"name":"web_search"}],"messages":[{"role":"assistant","content":"wrong"}]}`, strings.Replace(searchBody, "测试搜索", "一", 1), strings.Replace(searchBody, `"tools":`, `"stream":true,"tools":`, 1), strings.Replace(searchBody, `"tools":`, `"max_results":-1,"tools":`, 1), strings.Repeat(" ", 1<<20) + searchBody} {
		w := responseRequest(s, key, "/v1/search/messages", body)
		if w.Code != 400 && w.Code != 413 {
			t.Fatalf("invalid request accepted %d", w.Code)
		}
	}
	s.Store.UpdateSettings(func(st *store.Settings) { st.WorkBuddy.SearchEnabled = false })
	if w := responseRequest(s, key, "/v1/search/messages", searchBody); w.Code != 403 {
		t.Fatal("disabled search accepted")
	}
	if calls != 0 {
		t.Fatal("validation called upstream")
	}
}
func TestSearchGroupScopeWhitelistAndPoolLease(t *testing.T) {
	calls := 0
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer wb-access-cn" {
			t.Error("cross-region credential")
		}
		io.WriteString(w, searchFixture)
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	addWB(t, s, "global", time.Now().Add(time.Hour).Unix())
	g, err := s.Store.SaveGroup(store.AccountGroup{Name: "search", AccountIDs: []string{"wb-cn"}, AllowlistEnabled: true, ModelIDs: []string{"workbuddy/cn/shared"}})
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.Store.AddKeyInGroupAndSave("scoped", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w := responseRequest(s, key.Key, "/v1/search/messages", searchBody); w.Code != 403 {
		t.Fatalf("allowlist bypass: %d", w.Code)
	}
	g.ModelIDs = []string{"workbuddy/cn/web-search"}
	if _, err = s.Store.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	if w := responseRequest(s, key.Key, "/v1/search/messages", searchBody); w.Code != 200 {
		t.Fatalf("allowed search rejected: %d %s", w.Code, w.Body.String())
	}
	if w := responseRequest(s, key.Key, "/v1/search/messages", strings.Replace(searchBody, "workbuddy/cn/", "workbuddy/global/", 1)); w.Code != 403 {
		t.Fatal("cross-group search permitted")
	}
	_, release1 := s.Store.AcquireAccountForModel("", "workbuddy/cn/shared", nil)
	defer release1()
	_, release2 := s.Store.AcquireAccountForModel("", "workbuddy/cn/shared", nil)
	defer release2()
	if w := responseRequest(s, key.Key, "/v1/search/messages", searchBody); w.Code != 503 {
		t.Fatalf("search bypassed shared account concurrency: %d", w.Code)
	}
	if calls != 1 {
		t.Fatalf("unexpected calls %d", calls)
	}
}
func TestSearchRotationCoolingAndRefresh(t *testing.T) {
	calls, refreshes := 0, 0
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/plugin/auth/token/refresh" {
			refreshes++
			io.WriteString(w, `{"code":0,"data":{"accessToken":"fresh","refreshToken":"fresh-refresh","expiresIn":3600}}`)
			return
		}
		calls++
		if calls == 1 {
			w.WriteHeader(429)
			io.WriteString(w, `{"code":6004}`)
			return
		}
		io.WriteString(w, searchFixture)
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	second := s.Store.Account("wb-cn")
	second.ID = "second"
	second.ExternalID = "second"
	second.Credentials = mustJSON(workbuddy.Credentials{AccessToken: "second", RefreshToken: "refresh", UID: "second", Realm: "cn", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	s.Store.UpsertAccount(second)
	s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 2 })
	w := responseRequest(s, key, "/v1/search/messages", searchBody)
	if w.Code != 200 || calls != 2 {
		t.Fatalf("rotation %d %s", w.Code, w.Body.String())
	}
	if s.Store.ModelHealthSnapshot()["wb-cn"]["workbuddy/cn/web-search"].CooldownUntil <= time.Now().Unix() {
		t.Fatal("search cooldown missing")
	}
	for _, state := range s.Store.PoolSnapshot() {
		if state.InFlight != 0 {
			t.Fatal("lease leaked on retry")
		}
	}
	if s.Store.Logs(1)[0].Attempts != 2 {
		t.Fatal("search attempts missing")
	}
	// Expiring token must refresh before the next search; do not use model calls.
	s.Store.ClearModelHealth("wb-cn")
	s.Store.SetAccountPaused("second", true)
	acct := s.Store.Account("wb-cn")
	acct.Credentials = mustJSON(workbuddy.Credentials{AccessToken: "old", RefreshToken: "refresh", UID: "wb-user", Realm: "cn", ExpiresAt: time.Now().Add(time.Second).Unix()})
	s.Store.UpsertAccount(acct)
	w = responseRequest(s, key, "/v1/search/messages", searchBody)
	if w.Code != 200 || refreshes != 1 {
		t.Fatalf("refresh %d %s, count %d", w.Code, w.Body.String(), refreshes)
	}
}
func TestWorkBuddyPromptFallbackBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, mode              string
		enabled, partial, twice bool
		calls                   int
	}{
		{"fallback", "passthrough", true, false, false, 2},
		{"disabled", "passthrough", false, false, false, 1},
		{"custom", "custom", true, false, false, 1},
		{"second-block", "passthrough", true, false, true, 2},
		{"partial", "passthrough", true, true, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var systems []string
			s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer wb-access-cn" {
					t.Error("changed account")
				}
				var body struct {
					Messages []struct{ Role, Content string }
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				systems = append(systems, body.Messages[0].Content)
				if calls == 1 && tc.partial {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {\"code\":11128,\"msg\":\"blocked by security policy\"}\n\n")
					return
				}
				if calls == 1 || tc.twice {
					w.WriteHeader(400)
					io.WriteString(w, `{"code":11128,"msg":"blocked by security policy"}`)
					return
				}
				io.WriteString(w, summaryFixture("fallback-ok", "stop", 8, 3))
			})
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			s.Store.UpdateSettings(func(st *store.Settings) {
				st.WorkBuddy.PromptMode = tc.mode
				st.WorkBuddy.PromptText = "CUSTOM"
				st.WorkBuddy.PromptFallback = tc.enabled
				st.WorkBuddy.Sanitize = false
				st.MaxRetries = 3
			})
			body := fmt.Sprintf(`{"model":"workbuddy/cn/shared","stream":%v,"messages":[{"role":"system","content":"CLIENT"},{"role":"user","content":"hello"}]}`, tc.partial)
			w := responseRequest(s, key, "/v1/chat/completions", body)
			if calls != tc.calls {
				t.Fatalf("wrong retry count %d: %d %s", calls, w.Code, w.Body.String())
			}
			if calls == 2 && systems[1] != requestpolicy.FallbackSystemPrompt {
				t.Fatal("fallback prompt missing")
			}
			if tc.mode == "custom" && systems[0] != "CUSTOM" {
				t.Fatal("custom prompt ignored")
			}
			if tc.name == "fallback" && (w.Code != 200 || !strings.Contains(w.Body.String(), "fallback-ok")) {
				t.Fatal("fallback failed")
			}
			if s.Store.Account("wb-cn").Failures != 0 || s.Store.Account("wb-cn").CooldownUntil != 0 {
				t.Fatal("content block penalized account")
			}
			if s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
				t.Fatal("lease leak")
			}
			if s.Store.Logs(1)[0].Attempts != tc.calls {
				t.Fatal("fallback attempt accounting")
			}
		})
	}
}
func TestWorkBuddyPolicyAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				if strings.Contains(string(raw), "CLIENT") || !strings.Contains(string(raw), "CUSTOM") || !strings.Contains(string(raw), "CLI for Claude") {
					t.Errorf("policy absent %s", raw)
				}
				io.WriteString(w, summaryFixture("OK", "stop", 2, 1))
			})
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			s.Store.UpdateSettings(func(st *store.Settings) { st.WorkBuddy.PromptText = "CUSTOM" })
			input := "You are Claude Code, Anthropic's official CLI for Claude."
			req := map[string]any{"model": "workbuddy/cn/shared", "max_tokens": 16, "messages": []any{map[string]any{"role": "system", "content": "CLIENT"}, map[string]any{"role": "user", "content": input}}}
			if path == "/v1/messages" {
				req["system"] = "CLIENT"
				req["messages"] = []any{map[string]any{"role": "user", "content": input}}
			}
			if path == "/v1/responses" {
				delete(req, "messages")
				req["instructions"] = "CLIENT"
				req["input"] = input
			}
			w := responseRequest(s, key, path, string(mustJSON(req)))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
func TestSearchCancellationReleasesLease(t *testing.T) {
	entered := make(chan struct{})
	unblock := make(chan struct{})
	defer close(unblock)
	var calls atomic.Int64
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/v1/search/messages", strings.NewReader(searchBody)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+key)
	done := make(chan struct{})
	go func() { defer close(done); s.Handler().ServeHTTP(httptest.NewRecorder(), req) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("search did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not release search")
	}
	if s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 || calls.Load() != 1 {
		t.Fatal("cancel leak/retry")
	}
}

func TestSearchMeteredBudgetsFailBeforeUpstream(t *testing.T) {
	calls := 0
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	for _, budget := range []store.KeyBudget{{MaxTokens: 100}, {MaxUSD: 10}} {
		if err := s.Store.SetKeyBudgetAndSave(key, budget, true); err != nil {
			t.Fatal(err)
		}
		w := responseRequest(s, key, "/v1/search/messages", searchBody)
		if w.Code != 402 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 0 {
		t.Fatal("unmetered search bypassed metered budget")
	}
}
func TestWorkBuddySettingsValidationAndHotReload(t *testing.T) {
	systems := []string{}
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		systems = append(systems, body.Messages[0].Content)
		io.WriteString(w, summaryFixture("OK", "stop", 2, 1))
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	before := s.Store.Settings().WorkBuddy
	for _, bad := range []string{`{"prompt_mode":"bad"}`, `{"search_scope":"bad"}`, `null`, `[]`, `{"sanitize":"yes"}`} {
		w := httptest.NewRecorder()
		s.handleAdminSettings(w, httptest.NewRequest("POST", "/admin/api/settings", strings.NewReader(`{"workbuddy":`+bad+`}`)))
		if w.Code != 400 || s.Store.Settings().WorkBuddy != before {
			t.Fatal("invalid settings changed state", w.Code)
		}
	}
	body := `{"model":"workbuddy/cn/shared","messages":[{"role":"system","content":"CLIENT"},{"role":"user","content":"hi"}]}`
	postSettings(t, s, `{"workbuddy":{"prompt_text":"CUSTOM"}}`)
	if w := responseRequest(s, key, "/v1/chat/completions", body); w.Code != 200 {
		t.Fatal(w.Code)
	}
	postSettings(t, s, `{"workbuddy":{"prompt_mode":"passthrough","sanitize":false,"prompt_fallback":false}}`)
	if w := responseRequest(s, key, "/v1/chat/completions", body); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if len(systems) != 2 || systems[0] != "CUSTOM" || systems[1] != "CLIENT" {
		t.Fatal("settings not hot applied", systems)
	}
	current := s.Store.Settings().WorkBuddy
	if !current.SearchEnabled || current.SearchScope != "cn" || current.PromptFallback || current.Sanitize {
		t.Fatal("partial update reset unrelated fields", current)
	}
}
