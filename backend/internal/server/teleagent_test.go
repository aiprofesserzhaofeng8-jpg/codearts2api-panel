package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/teleagent"
)

func teleagentFixture() map[string]any {
	return map[string]any{"token": "fixture:" + base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"accountId":"teleagent-fixture","exp":2000000000}`)) + ".signature", "api_key": "sk-teleagent-fixture", "device_id": "device-fixture", "install_id": "install-fixture", "app_version": "2.7.1"}
}

func TestTeleagentHelperHTTPPreviewConfirmReplaceAndCancel(t *testing.T) {
	verifications := 0
	s, consumer, g, _ := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != teleagent.UserPath {
			t.Error("unexpected login request")
			return
		}
		verifications++
		io.WriteString(w, `{"code":0,"data":{"accountId":"teleagent-fixture","accountName":"Browser verified"}}`)
	})
	c := teleagent.New()
	c.Origin = strings.TrimSuffix(g.API, "/openai/v1")
	_ = s.Providers.Register(c)
	call := func(body any) (int, map[string]json.RawMessage, string) {
		r := httptest.NewRequest("POST", "/admin/api/provider-login", strings.NewReader(string(mustJSON(body))))
		r.AddCookie(adminTestCookie(s, "admin-fixture"))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]json.RawMessage
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out, w.Body.String()
	}
	str := func(o map[string]json.RawMessage, key string) string {
		var v string
		_ = json.Unmarshal(o[key], &v)
		return v
	}
	start := map[string]any{"action": "start", "provider": "teleagent", "scope": "cn"}
	if w := responseRequest(s, consumer, "/admin/api/provider-login", string(mustJSON(start))); w.Code != 401 {
		t.Fatal("consumer started login")
	}
	status, flow, body := call(start)
	if status != 200 || str(flow, "url") != teleagent.LoginURL || len(str(flow, "helper_challenge")) != 43 {
		t.Fatal(status, body)
	}
	sid, challenge := str(flow, "session_id"), str(flow, "helper_challenge")
	result := func(ch string) string {
		return string(mustJSON(map[string]any{"version": 1, "provider": "teleagent", "challenge": ch, "credentials": teleagentFixture()}))
	}
	count := len(s.Store.Accounts())
	status, _, _ = call(map[string]any{"action": "check", "session_id": sid, "input": result("wrong")})
	if status == 200 || verifications != 0 || len(s.Store.Accounts()) != count {
		t.Fatal("wrong task accepted")
	}
	status, preview, body := call(map[string]any{"action": "check", "session_id": sid, "input": result(challenge)})
	if status != 200 || str(preview, "preview_id") == "" || str(preview, "name") != "Browser verified" || len(s.Store.Accounts()) != count || verifications != 1 {
		t.Fatal("preview saved or unverified", status, body)
	}
	for _, secret := range []string{challenge, "sk-teleagent-fixture", "device-fixture", ".signature"} {
		if strings.Contains(body, secret) {
			t.Fatal("preview leaked secret")
		}
	}
	status, _, _ = call(map[string]any{"action": "complete", "session_id": sid, "preview_id": "wrong"})
	if status != 409 || len(s.Store.Accounts()) != count {
		t.Fatal("unconfirmed save")
	}
	status, saved, body := call(map[string]any{"action": "complete", "session_id": sid, "preview_id": str(preview, "preview_id")})
	id := str(saved, "account_id")
	if status != 200 || id == "" || len(s.Store.Accounts()) != count+1 {
		t.Fatal("save failed", status, body)
	}
	a := s.Store.Account(id)
	if a.Source != "browser" || a.Provider != "teleagent" || a.Name != "Browser verified" {
		t.Fatal("browser identity lost")
	}
	status, again, _ := call(map[string]any{"action": "complete", "session_id": sid, "preview_id": str(preview, "preview_id")})
	if status != 200 || str(again, "account_id") != id {
		t.Fatal("save not idempotent")
	}
	// A second verified login cannot overwrite the existing account implicitly.
	_, other, _ := call(start)
	otherSID := str(other, "session_id")
	_, otherPreview, _ := call(map[string]any{"action": "check", "session_id": otherSID, "input": result(str(other, "helper_challenge"))})
	status, _, _ = call(map[string]any{"action": "complete", "session_id": otherSID, "preview_id": str(otherPreview, "preview_id")})
	if status != 409 {
		t.Fatal("implicit replacement")
	}
	status, _, _ = call(map[string]any{"action": "complete", "session_id": otherSID, "preview_id": str(otherPreview, "preview_id"), "replace_existing": true})
	if status != 200 || len(s.Store.Accounts()) != count+1 {
		t.Fatal("explicit replacement failed")
	}
	_, canceled, _ := call(start)
	call(map[string]any{"action": "cancel", "session_id": str(canceled, "session_id")})
	status, _, _ = call(map[string]any{"action": "check", "session_id": str(canceled, "session_id"), "input": result(str(canceled, "helper_challenge"))})
	if status != 410 {
		t.Fatal("canceled result accepted")
	}
}
func teleagentAccount(t *testing.T, s *Server, c *teleagent.Client) {
	t.Helper()
	raw, id, e := c.Normalize(mustJSON(teleagentFixture()))
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Store.UpsertAccountAndSave(&accounts.Account{ID: "teleagent-fixture", Provider: provider.TeleAgent, Scope: "cn", ExternalID: id.ID, Name: "TeleAgent fixture", Enabled: true, Credentials: raw}, false)
	if e != nil {
		t.Fatal(e)
	}
	models, e := c.Models(t.Context(), raw)
	if e != nil {
		t.Fatal(e)
	}
	s.Reg.ReplaceProvider(provider.TeleAgent, "cn", models)
}
func teleagentComplete(w http.ResponseWriter, tools bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta := map[string]any{"content": "TELEAGENT_COMPLETE"}
	finish := "stop"
	if tools {
		delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_fixture", "type": "function", "function": map[string]string{"name": "lookup", "arguments": `{"n":9007199254740993}`}}}}
		finish = "tool_calls"
	}
	for _, v := range []any{map[string]any{"id": "fixture", "model": "chat-pro", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}}, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}}, map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 4, "total_tokens": 16, "prompt_tokens_details": map[string]int{"cached_tokens": 3}}}} {
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(v))
	}
	io.WriteString(w, "data: [DONE]\n\n")
}
func TestTeleagentThreeProtocolsMappingAccountingAndPermissions(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			for _, tools := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%v/%v", path, stream, tools), func(t *testing.T) {
					chats := 0
					s, _, g, _ := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == teleagent.UserPath {
							io.WriteString(w, `{"code":0,"data":{"accountId":"teleagent-fixture"}}`)
							return
						}
						if r.URL.Path != teleagent.ChatPath {
							t.Error("unexpected endpoint", r.URL.Path)
						}
						chats++
						b, _ := io.ReadAll(r.Body)
						if !strings.Contains(string(b), "CONTEXT_FIXTURE") || !strings.Contains(string(b), `"model":"chat-pro"`) || r.Header.Get("X-Superagent-Signature") == "" {
							t.Error("request context/model/signature lost")
						}
						teleagentComplete(w, tools)
					})
					c := teleagent.New()
					c.Origin = strings.TrimSuffix(g.API, "/openai/v1")
					if e := s.Providers.Register(c); e != nil {
						t.Fatal(e)
					}
					teleagentAccount(t, s, c)
					group, e := s.Store.SaveGroup(storage.AccountGroup{Name: "TeleAgent group", AccountIDs: []string{"teleagent-fixture"}, AllowlistEnabled: true, ModelIDs: []string{"teleagent/cn/chat-pro"}, ModelMappings: []storage.ModelMapping{{Name: "teleagent-pro-1", Sources: []string{"teleagent/cn/chat-pro"}}}})
					if e != nil {
						t.Fatal(e)
					}
					k, e := s.Store.AddKeyInGroupAndSave("TeleAgent", group.ID)
					if e != nil {
						t.Fatal(e)
					}
					_ = s.Handler()
					markModelsFresh(s)
					models := modelIDsFromResponse(t, scopedModelRequest(s, k.Key))
					if len(models) != 1 || models[0] != "teleagent-pro-1" {
						t.Fatal("mapping failed", models)
					}
					body := map[string]any{"model": "teleagent-pro-1", "messages": []map[string]string{{"role": "user", "content": "CONTEXT_FIXTURE"}}, "stream": stream, "max_tokens": 64}
					if path == "/v1/responses" {
						delete(body, "messages")
						body["input"] = "CONTEXT_FIXTURE"
					}
					w := responseRequest(s, k.Key, path, string(mustJSON(body)))
					want := "TELEAGENT_COMPLETE"
					if tools {
						want = "9007199254740993"
					}
					if w.Code != 200 || !strings.Contains(w.Body.String(), want) || chats != 1 {
						t.Fatal(w.Code, w.Body.String(), chats)
					}
					logs := s.Store.Logs(1)
					if len(logs) != 1 || logs[0].Provider != provider.TeleAgent || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 || logs[0].Usage.CachedTokens != 3 || s.Store.PoolSnapshot()["teleagent-fixture"].InFlight != 0 {
						t.Fatal("accounting or lease failed", logs)
					}
					w = responseRequest(s, k.Key, "/v1/chat/completions", `{"model":"teleagent/cn/chat-flash","messages":[{"role":"user","content":"no"}]}`)
					if w.Code != 403 || chats != 1 {
						t.Fatal("raw model bypassed group permission", w.Code)
					}
				})
			}
		}
	}
}
func TestTeleagentImportQuotaAndSecretBoundary(t *testing.T) {
	s, key, g, _ := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case teleagent.UserPath:
			io.WriteString(w, `{"code":0,"data":{"accountId":"teleagent-fixture","accountName":"Fixture"}}`)
		case teleagent.CreditPath:
			io.WriteString(w, `{"code":200,"data":{"current_points_balance":"73.25"}}`)
		default:
			t.Error("unexpected request", r.URL.Path)
		}
	})
	c := teleagent.New()
	c.Origin = strings.TrimSuffix(g.API, "/openai/v1")
	_ = s.Providers.Register(c)
	body := string(mustJSON(map[string]any{"provider": "teleagent", "credentials": teleagentFixture()}))
	if w := responseRequest(s, key, "/admin/api/provider-import", body); w.Code != 401 {
		t.Fatal("consumer imported credentials")
	}
	r := httptest.NewRequest("POST", "/admin/api/provider-import", strings.NewReader(body))
	r.AddCookie(adminTestCookie(s, "admin-fixture"))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "sk-teleagent-fixture") {
		t.Fatal("import failed or leaked", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/admin/api/credits?fresh=1", nil)
	r.AddCookie(adminTestCookie(s, "admin-fixture"))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remaining":73.25`) {
		t.Fatal("quota projection failed", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/admin/api/state", nil)
	r.AddCookie(adminTestCookie(s, "admin-fixture"))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	for _, secret := range []string{"sk-teleagent-fixture", ".signature", "device-fixture", "install-fixture"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("state leaked credential field", secret)
		}
	}
	var state map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &state) != nil {
		t.Fatal("bad admin state")
	}
}
