package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"codearts2api/internal/store"
)

func TestLocalPanelUnlockAndRemoteIsolation(t *testing.T) {
	s := poolServer(t, 0)
	s.Store.UpdateSettings(func(c *store.Settings) { c.AdminPassword = "admin-secret" })
	h := s.Handler()
	request := func(method, path, remote, host, token, origin, site string, proxy bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		r.RemoteAddr = remote
		r.Host = host
		r.Header.Set(localPanelHeader, token)
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", site)
		if proxy {
			r.Header.Set("X-Forwarded-For", "203.0.113.1")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/", "127.0.0.1:5555", "127.0.0.1:10087", "", "", "none", false)
	m := regexp.MustCompile(`name="local-panel-session" content="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(m) != 2 || strings.Contains(w.Body.String(), "admin-secret") {
		t.Fatal("local panel missing capability or exposing password")
	}
	token := m[1]
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("local capability must never be cached")
	}
	for _, tc := range []struct {
		name, remote, host, origin, site string
		proxy, allowed                   bool
	}{
		{"local", "127.0.0.1:5555", "127.0.0.1:10087", "http://127.0.0.1:10087", "same-origin", false, true},
		{"localhost", "127.0.0.1:5555", "localhost:10087", "http://localhost:10087", "same-origin", false, true},
		{"remote", "203.0.113.1:5555", "127.0.0.1:10087", "", "", false, false},
		{"proxy", "127.0.0.1:5555", "127.0.0.1:10087", "", "", true, false},
		{"rebind", "127.0.0.1:5555", "attacker.example:10087", "", "", false, false},
		{"other-origin", "127.0.0.1:5555", "127.0.0.1:10087", "http://localhost:8888", "", false, false},
		{"cross-site", "127.0.0.1:5555", "127.0.0.1:10087", "", "cross-site", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request("GET", "/admin/api/state", tc.remote, tc.host, token, tc.origin, tc.site, tc.proxy)
			if (w.Code == 200) != tc.allowed {
				t.Fatalf("admin status=%d", w.Code)
			}
			page := request("GET", "/", tc.remote, tc.host, "", tc.origin, tc.site, tc.proxy)
			if strings.Contains(page.Body.String(), token) != tc.allowed {
				t.Fatal("capability issuance violates local boundary")
			}
		})
	}
	for _, token := range []string{"", "invalid"} {
		if w := request("GET", "/admin/api/state", "127.0.0.1:5555", "127.0.0.1:10087", token, "", "", false); w.Code != 401 {
			t.Fatal("uncredentialed API request allowed")
		}
	}
	// Actual login-start endpoint works without a password through the local UI.
	w = request("POST", "/admin/api/login/start", "127.0.0.1:5555", "127.0.0.1:10087", token, "http://127.0.0.1:10087", "same-origin", false)
	if w.Code != 200 {
		t.Fatalf("local login blocked: %s", w.Body.String())
	}
	// Cancel created flow via its identifier, without printing the authorization URL.
	match := regexp.MustCompile(`"session_id":"([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("login session missing")
	}
	s.Logins.Cancel(match[1])
	s.Store.AddKey("test")
	w = request("GET", "/v1/models", "127.0.0.1:5555", "127.0.0.1:10087", token, "", "", false)
	if w.Code != 401 {
		t.Fatal("panel capability bypasses model API key")
	}
	remote := httptest.NewRequest("GET", "/admin/api/state", nil)
	remote.RemoteAddr = "203.0.113.1:5555"
	remote.Header.Set("X-Admin-Password", "admin-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, remote)
	if w.Code != 200 {
		t.Fatal("remote password access broken")
	}
}

func TestLocalPanelRejectsRewrittenPort(t *testing.T) {
	s := poolServer(t, 0)
	r := httptest.NewRequest("GET", "http://127.0.0.1:9999/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10087}))
	if s.localPanelRequest(r) {
		t.Fatal("forwarded Host with different port accepted")
	}
}
