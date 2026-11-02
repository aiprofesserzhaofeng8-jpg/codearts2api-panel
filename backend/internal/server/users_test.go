package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

const userTestPassword = "user-test-strong-password"

var userTestHashOnce sync.Once
var userTestHash string

func createTestUser(t *testing.T, s *Server, name, group string) access.UserView {
	t.Helper()
	if s.Users == nil {
		s.Users = access.NewUsers(s.Store)
	}
	userTestHashOnce.Do(func() {
		var err error
		userTestHash, err = access.HashAdminPassword(userTestPassword)
		if err != nil {
			panic(err)
		}
	})
	u, err := s.Store.SaveUser(access.UserUpdate{Username: name, GroupID: group}, userTestHash)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func userHTTP(s *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.10:4000"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func loginTestUser(t *testing.T, s *Server, name string) *http.Cookie {
	t.Helper()
	w := userHTTP(s, nil, "POST", "/user/auth/login", `{"username":"`+name+`","password":"`+userTestPassword+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != userCookie || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/user" {
		t.Fatal(cookies)
	}
	return cookies[0]
}
func TestUsersHTTPIsolationSessionsAndAudit(t *testing.T) {
	s, adminKey, g := mappedFixture(t, nil)
	s.AllowLocalAdmin = false
	s.Store.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = "admin-fixture-password" })
	alice := createTestUser(t, s, "alice", g.ID)
	bob := createTestUser(t, s, "bob", g.ID)
	a := loginTestUser(t, s, "alice")
	b := loginTestUser(t, s, "bob")
	for _, path := range []string{"/admin/api/state", "/admin/api/keys", "/admin/api/users", "/admin/api/accounts", "/admin/api/audit", "/admin/api/settings", "/metrics"} {
		w := userHTTP(s, a, "GET", path, "")
		if w.Code != 401 {
			t.Fatal("user reached admin", path, w.Code)
		}
	}
	w := userHTTP(s, a, "POST", "/user/api/keys", `{"name":"alice-key"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	k := s.Store.UserKeys(alice.ID)[0]
	if len(s.Store.UserKeys(bob.ID)) != 0 {
		t.Fatal("ownership not isolated")
	}
	for _, path := range []string{"/user/api/profile", "/user/api/keys", "/user/api/models", "/user/api/usage?user_id=" + bob.ID} {
		w = userHTTP(s, a, "GET", path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), userTestHash) || strings.Contains(w.Body.String(), adminKey) || strings.Contains(w.Body.String(), k.Key) {
			t.Fatal("private projection", path, w.Code, w.Body.String())
		}
	}
	for _, action := range []string{"toggle", "delete"} {
		w = userHTTP(s, b, "DELETE", "/user/api/keys?ref="+keyReference(k.Key)+"&action="+action, "")
		if w.Code != 404 || !s.Store.ValidKey(k.Key) {
			t.Fatal("cross-owner mutation", w.Code)
		}
	}
	w = userHTTP(s, b, "POST", "/user/api/keys/reveal", `{"ref":"`+keyReference(k.Key)+`"}`)
	if w.Code != 404 {
		t.Fatal("cross-owner reveal", w.Code)
	}
	w = userHTTP(s, a, "POST", "/user/api/keys", `{"name":"escape","group_id":""}`)
	if w.Code != 400 {
		t.Fatal("owner supplied group accepted")
	}
	r := httptest.NewRequest("POST", "/user/api/keys", strings.NewReader(`{"name":"csrf"}`))
	r.AddCookie(a)
	r.Header.Set("Origin", "https://evil.invalid")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("CSRF", w.Code)
	}
	for _, path := range []string{"/user/auth/session", "/user/api/usage"} {
		r = httptest.NewRequest("GET", path, nil)
		r.AddCookie(a)
		r.Header.Set("Origin", "https://evil.invalid")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross-origin user metadata", path, w.Code)
		}
	}
	// An inference API key is never a user-console login credential.
	r = httptest.NewRequest("GET", "/user/api/keys", nil)
	r.Header.Set("Authorization", "Bearer "+k.Key)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("key became console identity")
	}
	for _, event := range s.Store.AuditEvents() {
		if event.Path == "/user/api/keys" && event.Status == 200 {
			if event.Actor != "user:"+alice.ID {
				t.Fatal("wrong audit actor", event)
			}
		}
	}
	// Password change invalidates all old sessions, retains owned API keys.
	w = userHTTP(s, a, "POST", "/user/api/password", `{"old_password":"`+userTestPassword+`","password":"new-strong-user-password"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if userHTTP(s, a, "GET", "/user/api/keys", "").Code != 401 || !s.Store.ValidKey(k.Key) {
		t.Fatal("password revocation semantics")
	}
	w = userHTTP(s, b, "POST", "/user/auth/logout", "")
	if w.Code != 200 || userHTTP(s, b, "GET", "/user/api/profile", "").Code != 401 {
		t.Fatal("logout not revoked")
	}
}
func TestUsersAllInferenceProtocolsKeepOwnerAndActualModel(t *testing.T) {
	for _, platform := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", platform, path, stream), func(t *testing.T) {
					s, _, g := mappedFixture(t, nil)
					onlyMappedPlatform(s, platform)
					u := createTestUser(t, s, "alice", g.ID)
					key, err := s.Store.AddUserKeyAndSave(u.ID, "client")
					if err != nil {
						t.Fatal(err)
					}
					w := responseRequest(s, key.Key, path, mappedBody(path, stream, mappedPublic))
					if w.Code != 200 {
						t.Fatal(w.Code, w.Body.String())
					}
					entry := s.Store.Logs(1)[0]
					if entry.UserID != u.ID || entry.Provider != platform || entry.Model == mappedPublic || !entry.Usage.Known {
						t.Fatal("ownership or actual-model accounting", entry)
					}
					page := s.Store.QueryUserUsage(u.ID, storage.UsageQuery{})
					if page.Total != 1 || page.Logs[0].Model != mappedPublic {
						t.Fatal("user usage", page)
					}
					if s.Store.Users()[0].BudgetUsage.Requests != 1 {
						t.Fatal("shared budget")
					}
					for _, v := range s.Store.PoolSnapshot() {
						if v.InFlight != 0 {
							t.Fatal("lease leaked")
						}
					}
					// Revocation is checked at authentication and inside atomic admission.
					off := false
					if _, err = s.Store.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, Enabled: &off}, ""); err != nil {
						t.Fatal(err)
					}
					w = responseRequest(s, key.Key, path, mappedBody(path, stream, mappedPublic))
					if w.Code != 401 {
						t.Fatal("disabled user inference", w.Code)
					}
				})
			}
		}
	}
}
func TestUsersDisableRevokesConsoleAndModelPermissions(t *testing.T) {
	s, _, g := mappedFixture(t, nil)
	u := createTestUser(t, s, "alice", g.ID)
	cookie := loginTestUser(t, s, "alice")
	if userHTTP(s, cookie, "GET", "/user/api/models", "").Code != 200 {
		t.Fatal("no models")
	}
	_, _, err := s.Users.Login(context.Background(), "ALICE", userTestPassword, "additional-session")
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := s.Store.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, Enabled: &off}, ""); err != nil {
		t.Fatal(err)
	}
	if userHTTP(s, cookie, "GET", "/user/api/models", "").Code != 401 {
		t.Fatal("stale session")
	}
	if _, ok := s.Users.Authenticate("additional-session"); ok {
		t.Fatal("other session survived disable")
	}
	if _, ok := access.NewUsers(s.Store).Authenticate("additional-session"); ok {
		t.Fatal("new runtime inherited sessions")
	}
}

func TestUsersLastDeletedKeyDoesNotReopenAnonymousInference(t *testing.T) {
	s, _, g := mappedFixture(t, nil)
	createTestUser(t, s, "alice", g.ID)
	for _, key := range s.Store.Keys() {
		s.Store.DeleteKey(key.Key)
	}
	s.Store.UpdateSettings(func(st *storage.Settings) { st.AllowAnonymous = true })
	r := httptest.NewRequest("GET", "/v1/models", nil)
	r.RemoteAddr = "127.0.0.1:4000"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("users bypassed via anonymous fallback", w.Code)
	}
}

// An omitted model must retain the effective public default in the user's
// history, even after mappings are edited; administrator logs keep the actual ID.
func TestUsersDefaultModelUsageKeepsPublicName(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			s, _, g := mappedFixture(t, nil)
			g.DefaultModel = mappedPublic
			if _, err := s.Store.SaveGroup(g); err != nil {
				t.Fatal(err)
			}
			u := createTestUser(t, s, "alice", g.ID)
			k, err := s.Store.AddUserKeyAndSave(u.ID, "default")
			if err != nil {
				t.Fatal(err)
			}
			w := responseRequest(s, k.Key, path, mappedBody(path, false, ""))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			e := s.Store.Logs(1)[0]
			if e.RequestedModel != "" || e.PublicModel != mappedPublic || e.Model == mappedPublic {
				t.Fatal(e)
			}
			g.ModelMappings = nil
			g.DefaultModel = ""
			if _, err = s.Store.SaveGroup(g); err != nil {
				t.Fatal(err)
			}
			if got := s.Store.QueryUserUsage(u.ID, storage.UsageQuery{}).Logs[0].Model; got != mappedPublic {
				t.Fatal(got)
			}
		})
	}
}
