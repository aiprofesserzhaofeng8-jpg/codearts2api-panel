package server

import (
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
	"codearts2api/internal/trae"
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
)

func nativeTraeServer(t *testing.T, handler http.HandlerFunc) (*Server, string, *trae.Client) {
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
	up := httptest.NewServer(handler)
	t.Cleanup(up.Close)
	c := trae.New()
	c.AgentBase = up.URL
	c.OAuthBase = up.URL
	c.CreditBase = up.URL
	s.Providers = provider.NewRegistry()
	_ = s.Providers.Register(c)
	return s, st.AddKey("fixture").Key, c
}
func addTrae(t *testing.T, s *Server) {
	t.Helper()
	raw, _ := json.Marshal(trae.Credentials{AccessToken: "trae-private-fixture", RefreshToken: "trae-private-refresh", UID: "trae-user", Scope: "cn", ClientID: trae.ClientID, MachineID: "machine", DeviceID: "device", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	_, err := s.Store.UpsertAccountAndSave(&codearts.Account{ID: "trae-acct", Name: "Trae fixture", Provider: "trae", Scope: "cn", ExternalID: "trae-user", Credentials: raw, Enabled: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	s.Reg.ReplaceProvider("trae", "cn", []provider.Model{{ID: "shared", StreamOnly: true}})
}

const traeTextFixture = "event: output\ndata: {\"response\":\"TRAE_COMPLETE\",\"reasoning_content\":\"thought\"}\n\nevent: token_usage\ndata: {\"prompt_tokens\":12,\"completion_tokens\":4,\"reasoning_tokens\":2,\"cached_tokens\":3}\n\nevent: done\ndata: {\"finish_reason\":\"stop\"}\n\n"

func TestTraeThreeProtocolsAndAccounting(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", path, stream), func(t *testing.T) {
				calls := 0
				s, key, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != trae.ChatPath || r.Header.Get("Authorization") != "Cloud-IDE-JWT trae-private-fixture" {
						t.Error("credential/provider route crossed")
					}
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["config_name"] != "shared" || body["function"] != "solo_work_lite" || body["stream"] != true {
						t.Error("native payload missing")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, traeTextFixture)
				})
				addTrae(t, s)
				s.Store.UpsertAccount(&codearts.Account{ID: "ca", Enabled: true, Session: &codearts.Session{AccessKey: "ca-secret", SecretKey: "ca-secret"}})
				body := map[string]any{"model": "trae/cn/shared", "messages": []map[string]string{{"role": "user", "content": "hi"}}, "stream": stream, "max_tokens": 64}
				if path == "/v1/responses" {
					delete(body, "messages")
					body["input"] = "hi"
				}
				w := responseRequest(s, key, path, string(mustJSON(body)))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "TRAE_COMPLETE") || calls != 1 {
					t.Fatalf("completion failed: %d %s", w.Code, w.Body.String())
				}
				log := s.Store.Logs(1)[0]
				if log.Provider != "trae" || log.Model != "trae/cn/shared" || log.Usage.InputTokens != 12 || log.Usage.OutputTokens != 4 || log.Usage.CachedTokens != 3 || log.Usage.ReasoningTokens != 2 {
					t.Fatalf("wrong accounting: %+v", log)
				}
				if s.Store.PoolSnapshot()["trae-acct"].InFlight != 0 {
					t.Fatal("lease leaked")
				}
			})
		}
	}
}
func TestTraeToolOutputConversionAcrossProtocols(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			s, key, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				tools := body["tools"].([]any)
				fn := tools[0].(map[string]any)["function"].(map[string]any)
				if _, ok := fn["parameters"].(string); !ok {
					t.Error("native schema not serialized")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "event: output\ndata: {\"tool_calls\":[{\"index\":0,\"id\":\"call_fixture\",\"function_call\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"a\\\"}\"}}]}\n\nevent: token_usage\ndata: {\"prompt_tokens\":10,\"completion_tokens\":3}\n\nevent: done\ndata: {\"finish_reason\":\"tool_calls\"}\n\n")
			})
			addTrae(t, s)
			body := map[string]any{"model": "trae/cn/shared", "messages": []map[string]string{{"role": "user", "content": "Read a"}}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "parameters": map[string]any{"type": "object"}}}}, "max_tokens": 64}
			if path == "/v1/responses" {
				delete(body, "messages")
				body["input"] = "Read a"
				body["tools"] = []any{map[string]any{"type": "function", "name": "read", "parameters": map[string]any{"type": "object"}}}
			}
			if path == "/v1/messages" {
				body["tools"] = []any{map[string]any{"name": "read", "input_schema": map[string]any{"type": "object"}}}
			}
			w := responseRequest(s, key, path, string(mustJSON(body)))
			if w.Code != 200 || !strings.Contains(w.Body.String(), "call_fixture") || !strings.Contains(w.Body.String(), "read") {
				t.Fatalf("tool conversion failed: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestTraeCallbackPreviewConfirmCancelAndReplay(t *testing.T) {
	profiles := 0
	s, _, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != trae.UserPath {
			t.Error("unexpected upstream endpoint")
		}
		profiles++
		io.WriteString(w, `{"Result":{"UserID":"trae-user","ScreenName":"Trae verified"}}`)
	})
	start := providerAdmin(s, `{"action":"start","provider":"trae","scope":"cn"}`)
	if start.Code != 200 {
		t.Fatal(start.Body.String())
	}
	var flow map[string]any
	_ = json.Unmarshal(start.Body.Bytes(), &flow)
	uri, _ := url.Parse(flow["url"].(string))
	callback := uri.Query().Get("auth_callback_url")
	q := url.Values{"loginTraceID": {uri.Query().Get("login_trace_id")}, "userInfo": {`{"UserID":"trae-user"}`}, "userJwt": {fmt.Sprintf(`{"Token":"private-login-token","TokenExpireAt":%d}`, time.Now().Add(time.Hour).UnixMilli())}}
	check := func(input string) *httptest.ResponseRecorder {
		return providerAdmin(s, string(mustJSON(map[string]any{"action": "check", "session_id": flow["session_id"], "input": input})))
	}
	if w := check(""); w.Code != 200 || !strings.Contains(w.Body.String(), `"ready":false`) {
		t.Fatal("empty callback should stay pending")
	}
	w := check(callback + "?" + q.Encode())
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-login-token") {
		t.Fatal("preview failed/leaked token")
	}
	var preview map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if len(s.Store.Accounts()) != 0 {
		t.Fatal("preview automatically saved")
	}
	w = check(callback + "?" + q.Encode())
	if w.Code != 200 || profiles != 1 {
		t.Fatal("preview revalidated unnecessarily")
	}
	if bad := check(callback + "?" + q.Encode() + "&changed=1"); bad.Code != 409 {
		t.Fatal("different callback silently reused existing preview")
	}
	payload := string(mustJSON(map[string]any{"action": "complete", "session_id": flow["session_id"], "preview_id": preview["preview_id"]}))
	if w := providerAdmin(s, payload); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(s.Store.Accounts()) != 1 || s.Store.Accounts()[0].ProviderID() != "trae" {
		t.Fatal("provider account not saved")
	}
	s.Store.RemoveAccountAndSave(s.Store.Accounts()[0].ID)
	if w := providerAdmin(s, payload); w.Code != 410 {
		t.Fatal("deleted account resurrected")
	}
}
func TestTraeStreamFailureCannotReplayAndUnknownUsageRemainsUnknown(t *testing.T) {
	calls := 0
	s, key, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: output\ndata: {\"response\":\"partial-once\"}\n\n")
	})
	addTrae(t, s)
	s.Store.UpdateSettings(func(c *store.Settings) { c.MaxRetries = 3 })
	a := s.Store.Account("trae-acct")
	a.ID = "trae-second"
	a.ExternalID = "second"
	s.Store.UpsertAccount(a)
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"trae/cn/shared","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if calls != 1 || strings.Count(w.Body.String(), "partial-once") != 1 {
		t.Fatal("partial output replayed")
	}
	log := s.Store.Logs(1)[0]
	if log.Status < 400 || log.Usage.Known || !log.UsageIncomplete {
		t.Fatal("stream failure misreported")
	}
}

func TestTraeQuotaErrorIsNotCredentialFailure(t *testing.T) {
	s, key, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: error\ndata: {\"code\":1005,\"message\":\"limit\"}\n\n")
	})
	addTrae(t, s)
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"trae/cn/shared","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 402 || !strings.Contains(w.Body.String(), "insufficient_quota") {
		t.Fatal("upstream quota misclassified", w.Code, w.Body.String())
	}
}
