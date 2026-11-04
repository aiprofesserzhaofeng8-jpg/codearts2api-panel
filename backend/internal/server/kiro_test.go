package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/kiro"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/workbuddy"
)

func nativeKiroServer(t *testing.T, handler http.HandlerFunc) (*Server, string, *kiro.Client) {
	t.Helper()
	st, e := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	st.UpdateSettings(func(c *storage.Settings) {
		c.AdminPassword = "admin-fixture"
		c.RateLimitEnabled = false
		c.MaxRetries = 1
	})
	s := newConfiguredServer(st)
	up := httptest.NewServer(handler)
	t.Cleanup(up.Close)
	c := kiro.New()
	for scope := range c.Regions {
		c.Regions[scope] = kiro.Region{Runtime: up.URL, Catalog: up.URL, OIDC: up.URL, Social: up.URL}
	}
	s.Providers = provider.NewRegistry()
	if e := s.Providers.Register(c); e != nil {
		t.Fatal(e)
	}
	return s, st.AddKey("kiro fixture").Key, c
}
func addKiro(t *testing.T, s *Server, c *kiro.Client, scope string, key bool) {
	t.Helper()
	a := kiro.Credentials{AccessToken: "kiro-access-fixture", RefreshToken: "kiro-refresh-fixture", AuthMethod: "social", Region: scope, AuthRegion: scope, ProfileARN: "arn:aws:codewhisperer:" + scope + ":123:profile/fixture", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if key {
		a = kiro.Credentials{APIKey: "ksk_fixture", Region: scope}
	}
	raw, id, e := c.Normalize(mustJSON(a))
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Store.UpsertAccountAndSave(&accounts.Account{ID: "kiro-" + scope, Name: "Kiro fixture", Provider: provider.Kiro, Scope: scope, ExternalID: id.ID, Credentials: raw, Enabled: true}, false)
	if e != nil {
		t.Fatal(e)
	}
	s.Reg.ReplaceProvider(provider.Kiro, scope, []provider.Model{{ID: "kmodel", StreamOnly: true}})
}
func kiroFrame(kind, payload string) []byte {
	var h bytes.Buffer
	for k, v := range map[string]string{":message-type": "event", ":event-type": kind} {
		h.WriteByte(byte(len(k)))
		h.WriteString(k)
		h.WriteByte(7)
		_ = binary.Write(&h, binary.BigEndian, uint16(len(v)))
		h.WriteString(v)
	}
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b, uint32(16+h.Len()+len(payload)))
	binary.BigEndian.PutUint32(b[4:], uint32(h.Len()))
	binary.BigEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b[:8]))
	b = append(b, h.Bytes()...)
	b = append(b, []byte(payload)...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
}
func kiroComplete(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	w.Write(kiroFrame("assistantResponseEvent", `{"content":"KIRO_COMPLETE"}`))
	w.Write(kiroFrame("meteringEvent", `{"usage":0.2,"inputTokens":12,"outputTokens":4,"cacheReadInputTokens":3}`))
}
func TestKiroThreeProtocolsRegionsLifetimeAndAccounting(t *testing.T) {
	for _, scope := range []string{"us-east-1", "eu-central-1"} {
		for _, keyAuth := range []bool{false, true} {
			for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%v/%s/%v", scope, keyAuth, path, stream), func(t *testing.T) {
						calls := 0
						s, key, c := nativeKiroServer(t, func(w http.ResponseWriter, r *http.Request) {
							calls++
							wantPath, wantTarget := "/generateAssistantResponse", ""
							if keyAuth {
								wantPath, wantTarget = "/", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse"
							}
							if r.URL.Path != wantPath || r.Header.Get("X-Amz-Target") != wantTarget {
								t.Error("wrong native endpoint", r.URL.Path)
							}
							want := "Bearer kiro-access-fixture"
							if keyAuth {
								want = "Bearer ksk_fixture"
								if r.Header.Get("TokenType") != "API_KEY" {
									t.Error("API key type missing")
								}
							}
							if r.Header.Get("Authorization") != want {
								t.Error("wrong credentials")
							}
							var b struct {
								State struct {
									Current struct {
										User struct {
											Model string `json:"modelId"`
										} `json:"userInputMessage"`
									} `json:"currentMessage"`
								} `json:"conversationState"`
							}
							_ = json.NewDecoder(r.Body).Decode(&b)
							if b.State.Current.User.Model != "kmodel" {
								t.Error("namespace leaked to native model")
							}
							kiroComplete(w)
						})
						addKiro(t, s, c, scope, keyAuth)
						s.Store.UpdateSettings(func(c *storage.Settings) { c.AutoRefresh = false })
						body := map[string]any{"model": "kiro/" + scope + "/kmodel", "messages": []map[string]string{{"role": "user", "content": "hi"}}, "stream": stream, "max_tokens": 64}
						if path == "/v1/responses" {
							delete(body, "messages")
							body["input"] = "hi"
						}
						w := responseRequest(s, key, path, string(mustJSON(body)))
						if w.Code != 200 || !strings.Contains(w.Body.String(), "KIRO_COMPLETE") || calls != 1 {
							t.Fatal(w.Code, w.Body.String(), calls)
						}
						log := s.Store.Logs(1)[0]
						if log.Provider != provider.Kiro || log.Scope != scope || log.Model != body["model"] || log.Usage.InputTokens != 12 || log.Usage.OutputTokens != 4 || log.Usage.CachedTokens != 3 {
							t.Fatalf("incorrect accounting: %+v", log)
						}
						if s.Store.PoolSnapshot()["kiro-"+scope].InFlight != 0 {
							t.Fatal("lease leaked")
						}
					})
				}
			}
		}
	}
}
func TestKiroToolOutputAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", path, stream), func(t *testing.T) {
				s, key, c := nativeKiroServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
					for _, p := range []string{`{"toolUseId":"call_fixture","name":"read"}`, `{"toolUseId":"call_fixture","input":"{\"id\":9007199254740993}"}`, `{"toolUseId":"call_fixture","stop":true}`} {
						w.Write(kiroFrame("toolUseEvent", p))
					}
				})
				addKiro(t, s, c, "us-east-1", true)
				body := map[string]any{"model": "kiro/us-east-1/kmodel", "messages": []map[string]string{{"role": "user", "content": "read"}}, "stream": stream, "max_tokens": 64, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "parameters": map[string]any{"type": "object"}}}}}
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
					t.Fatal(w.Code, w.Body.String())
				}
			})
		}
	}
}
func TestKiroDevicePreviewExplicitSaveAndAdminBoundary(t *testing.T) {
	polls := 0
	s, key, _ := nativeKiroServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/register":
			io.WriteString(w, `{"clientId":"private-client-fixture","clientSecret":"private-secret-fixture"}`)
		case "/device_authorization":
			io.WriteString(w, `{"deviceCode":"private-device-fixture","expiresIn":600,"verificationUriComplete":"https://view.awsapps.com/start/#/device?user_code=CODE"}`)
		case "/token":
			polls++
			io.WriteString(w, `{"accessToken":"private-access-fixture","refreshToken":"private-refresh-fixture","expiresIn":3600}`)
		default:
			t.Error("unexpected request", r.URL.Path)
		}
	})
	startBody := `{"action":"start","provider":"kiro","scope":"us-east-1"}`
	if w := responseRequest(s, key, "/admin/api/provider-login", startBody); w.Code != 401 {
		t.Fatal("API key granted admin")
	}
	w := providerAdmin(s, startBody)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var flow map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &flow)
	check := string(mustJSON(map[string]any{"action": "check", "session_id": flow["session_id"]}))
	w = providerAdmin(s, check)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-") || len(s.Store.Accounts()) != 0 {
		t.Fatal("preview saved or leaked credentials", w.Body.String())
	}
	var preview map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if w = providerAdmin(s, check); w.Code != 200 || polls != 1 {
		t.Fatal("device code replayed")
	}
	complete := string(mustJSON(map[string]any{"action": "complete", "session_id": flow["session_id"], "preview_id": preview["preview_id"]}))
	if w = providerAdmin(s, complete); w.Code != 200 || len(s.Store.Accounts()) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e := s.Store.RemoveAccountAndSave(s.Store.Accounts()[0].ID); e != nil {
		t.Fatal(e)
	}
	if w = providerAdmin(s, complete); w.Code != 410 {
		t.Fatal("old login resurrected account", w.Code)
	}
}
func TestKiroStaticKeyImportVerifyNoRefreshAndMaskedState(t *testing.T) {
	calls := 0
	s, _, _ := nativeKiroServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/" || r.Header.Get("TokenType") != "API_KEY" {
			t.Error("unexpected import route")
		}
		io.WriteString(w, `{"models":[{"modelId":"kmodel"}]}`)
	})
	r := httptest.NewRequest("POST", "/admin/api/provider-import", strings.NewReader(`{"provider":"kiro","credentials":{"api_key":"ksk_private_fixture","region":"us-east-1"}}`))
	r.AddCookie(adminTestCookie(s, "admin-fixture"))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || calls != 1 || len(s.Store.Accounts()) != 1 {
		t.Fatal("static key import failed", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/admin/api/state", nil)
	r.AddCookie(adminTestCookie(s, "admin-fixture"))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "ksk_private_fixture") || !strings.Contains(w.Body.String(), `"non_expiring":true`) {
		t.Fatal("state leaked key or lost lifetime", w.Code, w.Body.String())
	}
	reloaded, e := storage.Open(s.Store.Path())
	if e != nil || len(reloaded.Accounts()) != 1 {
		t.Fatal("key not persisted", e)
	}
}
func TestKiroPartialBinaryStreamNotReplayed(t *testing.T) {
	calls := 0
	s, key, c := nativeKiroServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Write(kiroFrame("assistantResponseEvent", `{"content":"partial-once"}`))
		b := kiroFrame("meteringEvent", `{"inputTokens":12,"outputTokens":4}`)
		w.Write(b[:len(b)-2])
	})
	addKiro(t, s, c, "us-east-1", true)
	s.Store.UpdateSettings(func(c *storage.Settings) { c.MaxRetries = 3 })
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"kiro/us-east-1/kmodel","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if calls != 1 || strings.Count(w.Body.String(), "partial-once") != 1 {
		t.Fatal("partial output replayed", calls, w.Body.String())
	}
	log := s.Store.Logs(1)[0]
	if log.Status < 400 || !log.UsageIncomplete || log.Usage.Known || s.Store.PoolSnapshot()["kiro-us-east-1"].InFlight != 0 {
		t.Fatalf("failure/usage/lease lost: %+v", log)
	}
}

func TestKiroPromptSettingsPersistAndUnsupportedSearchFails(t *testing.T) {
	s, _, _ := nativeKiroServer(t, func(http.ResponseWriter, *http.Request) {
		t.Error("settings must not contact upstream")
	})
	update := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/admin/api/settings", strings.NewReader(body))
		r.AddCookie(adminTestCookie(s, "admin-fixture"))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := update(`{"common_prompt":{"prompt_mode":"custom","prompt_text":"COMMON","prompt_fallback":false,"sanitize":false},"kiro":{"prompt_overrides":{}}}`); w.Code != 200 {
		t.Fatal("common inheritance failed", w.Code, w.Body.String())
	}
	if p := s.Store.Settings().ProviderPolicy(provider.Kiro); p.PromptMode != "custom" || p.PromptText != "COMMON" || p.PromptFallback || p.Sanitize {
		t.Fatalf("wrong inherited policy: %+v", p)
	}
	if w := update(`{"kiro":{"prompt_overrides":{"prompt_text":"KIRO ONLY"}}}`); w.Code != 200 {
		t.Fatal("override failed", w.Code, w.Body.String())
	}
	if w := update(`{"kiro":{"search_enabled":true,"search_url":"https://search.invalid"}}`); w.Code != 400 {
		t.Fatal("unsupported search accepted", w.Code, w.Body.String())
	}
	reopened, err := storage.Open(s.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	p := reopened.Settings().ProviderPolicy(provider.Kiro)
	if p.PromptMode != "custom" || p.PromptText != "KIRO ONLY" || p.SearchEnabled {
		t.Fatalf("saved Kiro settings lost or invalid update persisted: %+v", p)
	}
}

func TestKiroUnifiedMappingAndGroupAccess(t *testing.T) {
	kiroCalls, wbCalls := 0, 0
	s, _, c := nativeKiroServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Target") == "AmazonCodeWhispererStreamingService.GenerateAssistantResponse" {
			kiroCalls++
			kiroComplete(w)
		} else {
			wbCalls++
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, summaryFixture("WB_COMPLETE", "stop", 10, 4))
		}
	})
	addKiro(t, s, c, "us-east-1", true)
	wb := workbuddy.New()
	wb.CNBase = c.Regions["us-east-1"].Runtime
	if e := s.Providers.Register(wb); e != nil {
		t.Fatal(e)
	}
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	g, e := s.Store.SaveGroup(storage.AccountGroup{Name: "fixture", AccountIDs: []string{"kiro-us-east-1", "wb-cn"}, AllowlistEnabled: true, ModelIDs: []string{"kiro/us-east-1/kmodel", "workbuddy/cn/shared"}, ModelMappings: []storage.ModelMapping{{Name: "fixture-1.0", Sources: []string{"kiro/us-east-1/kmodel", "workbuddy/cn/shared"}}}})
	if e != nil {
		t.Fatal(e)
	}
	key, e := s.Store.AddKeyInGroupAndSave("mapped", g.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = s.Handler()
	markModelsFresh(s)
	ids := modelIDsFromResponse(t, scopedModelRequest(s, key.Key))
	if len(ids) != 1 || ids[0] != "fixture-1.0" {
		t.Fatal("raw model leaked into menu", ids)
	}
	for _, p := range []string{provider.Kiro, provider.WorkBuddy} {
		onlyMappedPlatform(s, p)
		w := responseRequest(s, key.Key, "/v1/chat/completions", `{"model":"fixture-1.0","messages":[{"role":"user","content":"hi"}]}`)
		if w.Code != 200 || s.Store.Logs(1)[0].Provider != p {
			t.Fatal(p, w.Code, w.Body.String())
		}
	}
	onlyMappedPlatform(s, provider.Kiro)
	if w := responseRequest(s, key.Key, "/v1/chat/completions", `{"model":"kiro/us-east-1/kmodel","messages":[{"role":"user","content":"hi"}]}`); w.Code != 200 {
		t.Fatal("authorized hidden ID failed", w.Code, w.Body.String())
	}
	g.AccountIDs = []string{"wb-cn"}
	g.ModelIDs = []string{"workbuddy/cn/shared"}
	g.ModelMappings[0].Sources = g.ModelIDs
	if _, e = s.Store.SaveGroup(g); e != nil {
		t.Fatal(e)
	}
	if w := responseRequest(s, key.Key, "/v1/chat/completions", `{"model":"kiro/us-east-1/kmodel","messages":[{"role":"user","content":"hi"}]}`); w.Code != 403 {
		t.Fatal("group restriction bypassed", w.Code, w.Body.String())
	}
	if kiroCalls != 2 || wbCalls != 1 {
		t.Fatal("unexpected upstream calls", kiroCalls, wbCalls)
	}
}
