package server

import (
	"codearts2api/internal/storage"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoPasswordRejectsDNSRebinding(t *testing.T) {
	s := newTestServer(t, 0, 1)
	for _, path := range []string{"/admin/api/state", "/v1/models"} {
		r := httptest.NewRequest("GET", "http://attacker.invalid:10087"+path, nil)
		r.RemoteAddr = "127.0.0.1:45678"
		r.Header.Set("Origin", "http://attacker.invalid:10087")
		w := httptest.NewRecorder()
		if path == "/v1/models" {
			s.withAuth(okHandler())(w, r)
		} else {
			s.Handler().ServeHTTP(w, r)
		}
		if w.Code == 200 {
			t.Errorf("untrusted Host bypassed authentication on %s", path)
		}
	}
}

func TestAdminPasswordAttemptsAreLimited(t *testing.T) {
	s := newTestServer(t, 0, 1)
	s.Store.UpdateSettings(func(st *storage.Settings) { st.AdminPassword = "correct" })
	for i := 0; i < 21; i++ {
		r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"admin","password":"incorrect"}`))
		r.RemoteAddr = "203.0.113.1:1234"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		want := 401
		if i == 20 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d got %d want %d", i, w.Code, want)
		}
	}
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"admin","password":"correct"}`))
	r.RemoteAddr = "203.0.113.2:1234"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("unrelated peer blocked")
	}
}
