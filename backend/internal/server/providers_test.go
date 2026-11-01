package server

import (
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
	"codearts2api/internal/workbuddy"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func nativeWBServer(t *testing.T, fn http.HandlerFunc) (*Server, string, *workbuddy.Client) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateSettings(func(c *store.Settings) {
		c.AdminPassword = "admin-fixture"
		c.RateLimitEnabled = false
		c.MaxRetries = 1
	})
	s := New(st)
	up := httptest.NewServer(fn)
	t.Cleanup(up.Close)
	client := workbuddy.New()
	client.CNBase = up.URL
	client.GlobalBase = up.URL
	client.CNWeb = up.URL
	client.CNBilling = up.URL
	s.Providers = provider.NewRegistry()
	if err = s.Providers.Register(client); err != nil {
		t.Fatal(err)
	}
	return s, st.AddKey("client").Key, client
}
func addWB(t *testing.T, s *Server, scope string, expires int64) {
	t.Helper()
	raw, _ := json.Marshal(workbuddy.Credentials{AccessToken: "wb-access-" + scope, RefreshToken: "wb-refresh-" + scope, UID: "wb-user", Realm: scope, ExpiresAt: expires})
	_, err := s.Store.UpsertAccountAndSave(&codearts.Account{ID: "wb-" + scope, Name: "wb-" + scope, Provider: "workbuddy", Scope: scope, ExternalID: "wb-user", Credentials: raw, Enabled: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	s.Reg.ReplaceProvider("workbuddy", scope, []provider.Model{{ID: "shared", StreamOnly: true}})
}
func TestWorkBuddyThreeProtocolsAndProviderUsage(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%v", path, stream), func(t *testing.T) {
				calls := 0
				s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != "/v2/chat/completions" || r.Header.Get("Authorization") != "Bearer wb-access-cn" || r.Header.Get("X-Refresh-Token") != "" {
						t.Error("wrong upstream or credentials")
					}
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["model"] != "shared" {
						t.Error("namespaced model sent upstream")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, summaryFixture("WB_COMPLETE", "stop", 10, 4))
				})
				addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
				s.Store.UpsertAccount(&codearts.Account{ID: "ca", Enabled: true, Session: &codearts.Session{AccessKey: "must-not-leak", SecretKey: "must-not-leak"}})
				body := map[string]any{"model": "workbuddy/cn/shared", "messages": []map[string]string{{"role": "user", "content": "hi"}}, "stream": stream, "max_tokens": 64}
				if path == "/v1/responses" {
					delete(body, "messages")
					body["input"] = "hi"
				}
				w := responseRequest(s, key, path, string(mustJSON(body)))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "WB_COMPLETE") || calls != 1 {
					t.Fatalf("bad completion %d %s", w.Code, w.Body.String())
				}
				log := s.Store.Logs(1)[0]
				if log.Provider != "workbuddy" || log.Scope != "cn" || log.Model != "workbuddy/cn/shared" || log.Usage.InputTokens != 10 || log.Usage.OutputTokens != 4 {
					t.Fatalf("bad provider accounting: %+v", log)
				}
				if s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
					t.Fatal("lease not released")
				}
			})
		}
	}
}
func providerAdmin(s *Server, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/admin/api/provider-login", strings.NewReader(body))
	r.Header.Set("X-Admin-Password", "admin-fixture")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestWorkBuddyStagedLoginSaveReplayAndDeletion(t *testing.T) {
	var tokenCalls int
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			io.WriteString(w, `{"code":0,"data":{"state":"wb-state","authUrl":"https://www.codebuddy.cn/login?state=wb-state"}}`)
		case "/v2/plugin/auth/token":
			tokenCalls++
			io.WriteString(w, `{"code":0,"data":{"accessToken":"wb-access","refreshToken":"wb-refresh","expiresIn":7200}}`)
		case "/v2/plugin/login/account":
			io.WriteString(w, `{"code":0,"data":{"uid":"verified-user","nickname":"WorkBuddy fixture"}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	})
	start := providerAdmin(s, `{"action":"start","provider":"workbuddy","scope":"cn"}`)
	if start.Code != 200 {
		t.Fatal(start.Body.String())
	}
	var flow map[string]any
	_ = json.Unmarshal(start.Body.Bytes(), &flow)
	id := flow["session_id"].(string)
	check := func(input string) *httptest.ResponseRecorder {
		return providerAdmin(s, string(mustJSON(map[string]any{"action": "check", "session_id": id, "input": input})))
	}
	if w := check("http://localhost/callback?state=other"); w.Code != 400 {
		t.Fatal("cross-flow callback accepted")
	}
	w := check("")
	if w.Code != 200 || strings.Contains(w.Body.String(), "wb-access") || strings.Contains(w.Body.String(), "wb-refresh") {
		t.Fatal("preview leaked token or failed")
	}
	var preview map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if len(s.Store.Accounts()) != 0 {
		t.Fatal("check implicitly saved account")
	}
	check("")
	if tokenCalls != 1 {
		t.Fatal("repeated check exchanged token again")
	}
	complete := string(mustJSON(map[string]any{"action": "complete", "session_id": id, "preview_id": preview["preview_id"]}))
	if w := providerAdmin(s, complete); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(s.Store.Accounts()) != 1 {
		t.Fatal("account missing")
	}
	if w := providerAdmin(s, complete); w.Code != 200 || len(s.Store.Accounts()) != 1 {
		t.Fatal("save replay not idempotent")
	}
	acct := s.Store.Accounts()[0]
	if acct.ProviderID() != "workbuddy" || acct.Scope != "cn" || acct.Session != nil {
		t.Fatal("credential types mixed")
	}
	s.Store.RemoveAccountAndSave(acct.ID)
	if w := providerAdmin(s, complete); w.Code != 410 {
		t.Fatal("deleted account revived by old authorization")
	}
}
func TestWorkBuddySingleFlightAndCanceledWaiter(t *testing.T) {
	var calls atomic.Int32
	entered, proceed := make(chan struct{}), make(chan struct{})
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-proceed
		io.WriteString(w, `{"code":0,"data":{"accessToken":"rotated","refreshToken":"rotated-rt","expiresIn":7200}}`)
	})
	addWB(t, s, "cn", time.Now().Add(-time.Minute).Unix())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, err := s.providerCredentials(context.Background(), s.Store.Account("wb-cn"))
		if err != nil {
			t.Error(err)
		}
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.providerCredentials(ctx, s.Store.Account("wb-cn")); err == nil {
		t.Fatal("canceled waiter not stopped")
	}
	close(proceed)
	wg.Wait()
	_, _, err := s.providerCredentials(context.Background(), s.Store.Account("wb-cn"))
	if err != nil || calls.Load() != 1 {
		t.Fatal("refresh repeated with stale token")
	}
}
func TestProviderModelRefreshAndTraeRejection(t *testing.T) {
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/config" {
			t.Fatal(r.URL.Path)
		}
		io.WriteString(w, `{"code":0,"data":{"models":[{"id":"shared","name":"Shared","maxInputTokens":64000,"maxOutputTokens":4096}]}}`)
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	if err := s.RefreshModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Reg.Resolve("workbuddy/cn/shared", nil); !ok {
		t.Fatal("provider catalog missing")
	}
	if s.cache.loadedAt.IsZero() {
		t.Fatal("provider-only refresh not cached")
	}
	w := responseRequest(s, key, "/v1/responses", `{"model":"trae/cn/shared","input":"hello"}`)
	if w.Code != 404 {
		t.Fatal("unimplemented Trae silently routed")
	}
	if w := providerAdmin(s, `{"action":"start","provider":"trae"}`); w.Code != 400 {
		t.Fatal("unimplemented login accepted")
	}
}
func TestNoWorkBuddyCredentialCrossesCodeArtsManager(t *testing.T) {
	s := poolServer(t, 0)
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	if _, err := s.Cred.Session(context.Background(), s.Store.Account("wb-cn"), codearts.DefaultResolveOptions()); err == nil {
		t.Fatal("CodeArts consumed foreign credentials")
	}
	s.Reg.Replace([]registry.Model{{ID: "shared"}})
	if a, release := s.Store.AcquireAccountForModel("", "shared", nil); a != nil {
		release()
		t.Fatal("WorkBuddy used for unqualified CodeArts model")
	}
}

func TestProviderImportVerifiedAndStateNeverReturnsCredentials(t *testing.T) {
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/console/account" || r.Header.Get("x-client-platform") != "web" {
			t.Error("identity check missing")
		}
		io.WriteString(w, `{"code":0,"data":{"uid":"verified-user","nickname":"Verified"}}`)
	})
	credentials := map[string]any{"accessToken": "private-access-canary", "refreshToken": "private-refresh-canary", "expiresAt": time.Now().Add(time.Hour).Unix(), "uid": "verified-user", "realm": "cn"}
	importRequest := func(uid string) *httptest.ResponseRecorder {
		credentials["uid"] = uid
		req := httptest.NewRequest("POST", "/admin/api/provider-import", strings.NewReader(string(mustJSON(map[string]any{"provider": "workbuddy", "credentials": credentials}))))
		req.Header.Set("X-Admin-Password", "admin-fixture")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		return w
	}
	if w := importRequest("wrong-user"); w.Code != 400 {
		t.Fatal("mismatched identity imported")
	}
	if len(s.Store.Accounts()) != 0 {
		t.Fatal("failed import mutated accounts")
	}
	if w := importRequest("verified-user"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := importRequest("verified-user"); w.Code != 409 {
		t.Fatal("silent duplicate replacement")
	}
	req := httptest.NewRequest("GET", "/admin/api/state", nil)
	req.Header.Set("X-Admin-Password", "admin-fixture")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-access-canary") || strings.Contains(w.Body.String(), "private-refresh-canary") || strings.Contains(w.Body.String(), "provider_credentials") {
		t.Fatal("state leaked upstream credential")
	}
}
func TestProviderStreamFailureDoesNotReplay(t *testing.T) {
	calls := 0
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
	})
	s.Store.UpdateSettings(func(c *store.Settings) { c.MaxRetries = 3 })
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	a := s.Store.Account("wb-cn")
	a.ID = "wb-other"
	a.ExternalID = "other"
	s.Store.UpsertAccount(a)
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"workbuddy/cn/shared","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if calls != 1 || strings.Count(w.Body.String(), "partial") != 1 {
		t.Fatal("partial stream replayed")
	}
	if s.Store.Logs(1)[0].Status < 400 {
		t.Fatal("truncated stream logged as success")
	}
}
func TestProviderSummaryCannotCrossUpstreams(t *testing.T) {
	s := poolServer(t, 0)
	s.Reg.ReplaceProvider("workbuddy", "cn", []provider.Model{{ID: "shared"}})
	p, _ := s.Store.ModelPolicy("deepseek-v4.1-flash")
	p.SummaryFallbackModel = "workbuddy/cn/shared"
	r := httptest.NewRequest("POST", "/admin/api/model-policies", strings.NewReader(string(mustJSON(map[string]any{"model": "deepseek-v4.1-flash", "policy": p}))))
	w := httptest.NewRecorder()
	s.handleAdminModelPolicies(w, r)
	if w.Code != 400 {
		t.Fatal("cross-provider summary fallback accepted")
	}
}

func TestWorkBuddyClientProfileUsesCatalogBounds(t *testing.T) {
	s := poolServer(t, 0)
	s.Reg.ReplaceProvider("workbuddy", "cn", []provider.Model{{ID: "shared", Context: 64000, MaxOutput: 4096}})
	r := httptest.NewRequest("GET", "/v1/client-profile?model=workbuddy/cn/shared", nil)
	w := httptest.NewRecorder()
	s.handleClientProfile(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var p map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p["model"] != "workbuddy/cn/shared" || p["max_output_tokens"] != float64(4096) || p["pi_summary_adapter"] != false {
		t.Fatal("provider profile guessed unsupported capability")
	}
}

func TestExplicitProviderAliasCannotRedirectCredentials(t *testing.T) {
	s := poolServer(t, 0)
	settings := s.Store.Settings()
	settings.ModelAliases = map[string]string{"trae/cn/test": "test-model"}
	if _, err := s.resolveModel(settings, "trae/cn/test"); err == nil {
		t.Fatal("explicit provider redirected by alias")
	}
}
