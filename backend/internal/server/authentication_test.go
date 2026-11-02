package server

import (
	"codearts2api/internal/access"
	"codearts2api/internal/storage"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Business-handler fixtures use the same credential verification and session
// issuance as production. This helper is not part of the built application.
func adminTestCookie(s *Server, password string) *http.Cookie {
	if s.Users == nil {
		s.Users = access.NewUsers(s.Store, func() string { return s.Store.Settings().AdminPassword })
	}
	token := rand.Text() + rand.Text()
	if _, _, err := s.Users.Login(context.Background(), "admin", password, token); err != nil {
		panic(err)
	}
	return &http.Cookie{Name: sessionCookie, Value: token, Path: "/"}
}

// Older business fixtures have no persisted administrator. Give those isolated
// fixtures a known hashed credential; authorization still goes through Login.
var fixtureAdminHashOnce sync.Once
var fixtureAdminHash string
var fixtureAuthMu sync.Mutex
var fixtureCookies = map[string]*http.Cookie{}

func fixtureAdminCookie(s *Server) *http.Cookie {
	fixtureAuthMu.Lock()
	defer fixtureAuthMu.Unlock()
	key := s.Store.Path()
	if c := fixtureCookies[key]; c != nil && s.Users != nil {
		if _, ok := s.Users.Authenticate(c.Value); ok {
			return c
		}
	}
	fixtureAdminHashOnce.Do(func() {
		var err error
		fixtureAdminHash, err = access.HashAdminPassword("http-test-administrator")
		if err != nil {
			panic(err)
		}
	})
	s.Store.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = fixtureAdminHash })
	c := adminTestCookie(s, "http-test-administrator")
	fixtureCookies[key] = c
	return c
}

func TestUnifiedAuthenticationRejectsPrivilegeEscalation(t *testing.T) {
	s, _, group := mappedFixture(t, nil)
	s.Store.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = "fixture-admin" })
	createTestUser(t, s, "ordinary", group.ID)
	user := loginTestUser(t, s, "ordinary")
	admin := adminTestCookie(s, "fixture-admin")
	for _, tc := range []struct {
		cookie *http.Cookie
		path   string
		want   int
	}{
		{nil, "/admin/api/state", 401}, {user, "/admin/api/state", 403}, {admin, "/admin/api/state", 200},
		{admin, "/user/api/profile", 403}, {user, "/user/api/profile", 200},
	} {
		if w := userHTTP(s, tc.cookie, "GET", tc.path, ""); w.Code != tc.want {
			t.Fatalf("%s: got %d want %d", tc.path, w.Code, tc.want)
		}
	}
	for _, path := range []string{"/admin/api/settings", "/admin/api/users", "/admin/api/provider-import", "/admin/api/consumer/wallet", "/admin/api/consumer/identity"} {
		if w := userHTTP(s, user, "POST", path, `{"role":"admin"}`); w.Code != 403 {
			t.Fatal("user mutation", path, w.Code)
		}
	}
	for _, body := range []string{`{"username":"ordinary","password":"` + userTestPassword + `","role":"admin"}`, `{"username":"admin","password":"` + userTestPassword + `"}`, `{"username":"ordinary","password":"fixture-admin"}`} {
		w := userHTTP(s, nil, "POST", "/auth/login", body)
		if w.Code != 400 && w.Code != 401 {
			t.Fatal("forged login", w.Code)
		}
	}
	for _, path := range []string{"/admin/auth/login", "/admin/auth/session", "/admin/auth/logout", "/user/auth/login", "/user/auth/session", "/user/auth/logout"} {
		if w := userHTTP(s, admin, "POST", path, `{"password":"fixture-admin"}`); w.Code != 404 {
			t.Fatal("retired endpoint", path, w.Code)
		}
	}
	for _, headers := range []map[string]string{
		{"X-Admin-Password": "fixture-admin"}, {"Authorization": "Bearer fixture-admin"},
		{"X-Local-Panel-Session": "forged"}, {"X-Role": "admin", "X-User-ID": access.BuiltinAdminID},
		{"Cookie": "codearts_admin_session=forged; codearts_user_session=forged"},
	} {
		for _, cookie := range []*http.Cookie{nil, user} {
			r := httptest.NewRequest("GET", "http://127.0.0.1:10088/admin/api/state", nil)
			r.RemoteAddr = "127.0.0.1:1234"
			for key, value := range headers {
				r.Header.Set(key, value)
			}
			if cookie != nil {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			want := 401
			if cookie != nil {
				want = 403
			}
			if w.Code != want {
				t.Fatal("legacy/forged credentials", w.Code)
			}
		}
	}
	w := userHTTP(s, user, "GET", "/auth/session", "")
	var session struct{ User access.UserView }
	json.Unmarshal(w.Body.Bytes(), &session)
	if session.User.Role != access.RoleUser || strings.Contains(w.Body.String(), "fixture-admin") {
		t.Fatal("session role or secret leak")
	}
	if w = userHTTP(s, user, "POST", "/user/api/profile", `{"role":"admin"}`); w.Code != 400 {
		t.Fatal("role accepted by profile", w.Code)
	}
	// The same browser changes identity through credential verification, never
	// through a role parameter. Its previous token must no longer authorize.
	w = userHTTP(s, user, "POST", "/auth/login", `{"username":"admin","password":"fixture-admin"}`)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("unified administrator login", w.Code)
	}
	if userHTTP(s, user, "GET", "/user/api/profile", "").Code != 401 {
		t.Fatal("replaced consumer token remained valid")
	}
	replacement := w.Result().Cookies()[0]
	if userHTTP(s, replacement, "GET", "/admin/api/state", "").Code != 200 {
		t.Fatal("replacement session cannot access administrator API")
	}
	if w = userHTTP(s, user, "POST", "/auth/oauth/start", `{"provider":"test","bind":true}`); w.Code == 200 {
		t.Fatal("unconfigured OAuth unexpectedly available")
	}
}
