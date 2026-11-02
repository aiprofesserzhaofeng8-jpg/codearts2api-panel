package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/storage"
)

func TestProviderLoginHTTPErrorAndRequestBoundaries(t *testing.T) {
	for _, test := range []struct {
		kind   accounts.LoginErrorKind
		status int
		typ    string
	}{
		{accounts.LoginInvalid, 400, "invalid_request_error"}, {accounts.LoginMissing, 404, "invalid_request_error"},
		{accounts.LoginExpired, 410, "invalid_request_error"}, {accounts.LoginConflict, 409, "invalid_request_error"},
		{accounts.LoginInternal, 500, "server_error"}, {accounts.LoginCapacity, 429, "rate_limit_error"}, {accounts.LoginUpstream, 502, "upstream_error"},
	} {
		w := httptest.NewRecorder()
		writeLoginError(w, &accounts.LoginError{Kind: test.kind, Message: "fixture error"})
		var body struct {
			Error struct{ Message, Type string }
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != test.status || body.Error.Type != test.typ || body.Error.Message != "fixture error" {
			t.Fatalf("error mapping: %d %s", w.Code, w.Body.String())
		}
	}
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = "fixture-admin" })
	s := newConfiguredServer(st)
	for _, route := range []string{"/admin/api/provider-login", "/admin/api/provider-import"} {
		for _, test := range []struct {
			method, body, auth string
			status             int
		}{
			{"POST", `{}`, "", 401}, {"GET", `{}`, "fixture-admin", 405},
			{"POST", `{`, "fixture-admin", 400}, {"POST", `{"input":"` + strings.Repeat("x", 257<<10) + `"}`, "fixture-admin", 400},
		} {
			r := httptest.NewRequest(test.method, route, strings.NewReader(test.body))
			if test.auth != "" {
				r.AddCookie(adminTestCookie(s, test.auth))
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("%s %s want %d got %d", test.method, route, test.status, w.Code)
			}
		}
	}
	if len(st.Accounts()) != 0 {
		t.Fatal("invalid request mutated accounts")
	}
}

func TestProviderLoginPersistenceFailureCanRetryWithoutExchange(t *testing.T) {
	calls := 0
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			io.WriteString(w, `{"code":0,"data":{"state":"fixture-state","authUrl":"https://www.codebuddy.cn/login?state=fixture-state"}}`)
		case "/v2/plugin/auth/token":
			calls++
			io.WriteString(w, `{"code":0,"data":{"accessToken":"fixture-access","refreshToken":"fixture-refresh","expiresIn":7200}}`)
		case "/v2/plugin/login/account":
			io.WriteString(w, `{"code":0,"data":{"uid":"fixture-user","nickname":"Fixture"}}`)
		default:
			t.Errorf("unexpected upstream route %s", r.URL.Path)
		}
	})
	start := providerAdmin(s, `{"action":"start","provider":"workbuddy"}`)
	if start.Code != 200 {
		t.Fatal(start.Body.String())
	}
	var started, preview map[string]any
	_ = json.Unmarshal(start.Body.Bytes(), &started)
	check := providerAdmin(s, string(mustJSON(map[string]any{"action": "check", "session_id": started["session_id"]})))
	if check.Code != 200 {
		t.Fatal(check.Body.String())
	}
	_ = json.Unmarshal(check.Body.Bytes(), &preview)
	body := string(mustJSON(map[string]any{"action": "complete", "session_id": started["session_id"], "preview_id": preview["preview_id"]}))
	dir := filepath.Dir(s.Store.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	restored := false
	t.Cleanup(func() {
		if !restored {
			_ = os.Remove(dir)
			_ = os.Rename(dir+"-saved", dir)
		}
	})
	if err := os.WriteFile(dir, []byte("fixture blocks persistence"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := providerAdmin(s, body)
	if failed.Code != 409 || !strings.Contains(failed.Body.String(), "保存失败") {
		t.Fatalf("failed save: %d %s", failed.Code, failed.Body.String())
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"-saved", dir); err != nil {
		t.Fatal(err)
	}
	restored = true
	saved := providerAdmin(s, body)
	if saved.Code != 200 || calls != 1 || len(s.Store.Accounts()) != 1 {
		t.Fatalf("retry: %d calls %d", saved.Code, calls)
	}
	if replay := providerAdmin(s, body); replay.Code != 200 || len(s.Store.Accounts()) != 1 || calls != 1 {
		t.Fatal("save replay mutated accounts")
	}
	reloaded, err := storage.Open(s.Store.Path())
	if err != nil || len(reloaded.Accounts()) != 1 {
		t.Fatal("saved account not recoverable", err)
	}
}
