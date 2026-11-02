package server

import (
	"codearts2api/internal/storage"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAdminSessionLifecycleAndAudit(t *testing.T) {
	s := poolServer(t, 0)
	s.AllowLocalAdmin = false
	s.Store.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = "session-test-secret" })
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Store.Path())
	if strings.Contains(string(raw), "session-test-secret") {
		t.Fatal("plaintext password saved")
	}
	h := s.Handler()
	call := func(method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://panel.example"+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.TLS = &tls.ConnectionState{}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/admin/api/state", "", nil, ""); w.Code != 401 {
		t.Fatal("default loopback bypassed login")
	}
	if w := call("POST", "/admin/auth/login", `{"password":"session-test-secret"}`, nil, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-site login accepted")
	}
	if w := call("POST", "/admin/auth/login", `{"password":"wrong"}`, nil, ""); w.Code != 401 {
		t.Fatal("wrong password accepted")
	}
	login := func() *http.Cookie {
		w := call("POST", "/admin/auth/login", `{"password":"session-test-secret"}`, nil, "")
		if w.Code != 200 {
			t.Fatalf("login=%d", w.Code)
		}
		c := w.Result().Cookies()[0]
		if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.MaxAge != 28800 {
			t.Fatal("unsafe session cookie")
		}
		return c
	}
	cookie := login()
	if w := call("GET", "/admin/api/state", "", cookie, ""); w.Code != 200 {
		t.Fatal("session cannot read state")
	}
	if w := call("POST", "/admin/api/keys", `{"name":"never-log-this-body"}`, cookie, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-site mutation accepted")
	}
	if w := call("POST", "/admin/api/keys", `{"name":"never-log-this-body"}`, cookie, ""); w.Code != 200 {
		t.Fatal("session cannot mutate")
	}
	if w := call("POST", "/admin/auth/logout", "", cookie, ""); w.Code != 200 {
		t.Fatal("logout failed")
	}
	if w := call("GET", "/admin/api/state", "", cookie, ""); w.Code != 401 {
		t.Fatal("logout did not revoke")
	}
	cookie = login()
	s.AdminSessions.Issue(cookie.Value, s.Store.Settings().AdminPassword, time.Now().Add(-9*time.Hour))
	if w := call("GET", "/admin/api/state", "", cookie, ""); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
	cookie = login()
	s.Store.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = "new-password" })
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/admin/api/state", "", cookie, ""); w.Code != 401 {
		t.Fatal("password change did not revoke")
	}
	audit, _ := json.Marshal(s.Store.AuditEvents())
	for _, secret := range []string{"session-test-secret", "never-log-this-body", cookie.Value} {
		if strings.Contains(string(audit), secret) {
			t.Fatal("audit leaked secret")
		}
	}
	events := s.Store.AuditEvents()
	if len(events) < 5 {
		t.Fatal("missing login/mutation audit")
	}
	loaded, err := storage.Open(s.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !storage.VerifyAdminPassword(loaded.Settings().AdminPassword, "new-password") {
		t.Fatal("password migration failed")
	}
}
func TestDefaultAdminCannotClearPassword(t *testing.T) {
	s := newTestServer(t, 0, 1)
	s.AllowLocalAdmin = false
	w := httptest.NewRecorder()
	s.handleAdminSettings(w, httptest.NewRequest("POST", "/admin/api/settings", strings.NewReader(`{"clear_admin_password":true}`)))
	if w.Code != 400 {
		t.Fatal("password cleared in mandatory-login mode")
	}
}
