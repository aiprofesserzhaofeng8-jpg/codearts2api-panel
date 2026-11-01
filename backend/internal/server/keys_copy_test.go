package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/store"
)

func TestAdminCanRetrieveExistingKeysForCopy(t *testing.T) {
	s := poolServer(t, 0)
	s.Store.UpdateSettings(func(c *store.Settings) { c.AdminPassword = "admin" })
	key := s.Store.AddKey("copy-test").Key
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Open(s.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	s = New(loaded)
	for _, path := range []string{"/admin/api/state", "/admin/api/keys"} {
		for _, auth := range []struct {
			password string
			allowed  bool
		}{{"admin", true}, {key, false}, {"", false}} {
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("X-Admin-Password", auth.password)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if !auth.allowed {
				if w.Code != 401 {
					t.Fatal("non-admin accessed stored keys")
				}
				continue
			}
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("copy response unavailable or cacheable")
			}
			var body struct{ Keys []struct{ Key, Value string } }
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Keys) != 1 || body.Keys[0].Value != "" || body.Keys[0].Key == key {
				t.Fatal("list exposed key or masked reference missing")
			}
		}
	}
}

func TestExplicitRevealRequiresAdminAndPOST(t *testing.T) {
	s := poolServer(t, 0)
	s.Store.UpdateSettings(func(c *store.Settings) { c.AdminPassword = "admin" })
	key := s.Store.AddKey("test").Key
	for _, tc := range []struct {
		method, pw string
		want       int
	}{{"POST", "admin", 200}, {"GET", "admin", 405}, {"POST", key, 401}, {"POST", "", 401}} {
		r := httptest.NewRequest(tc.method, "/admin/api/keys/reveal", strings.NewReader(`{"key":"`+keyReference(key)+`"}`))
		r.Header.Set("X-Admin-Password", tc.pw)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("status %d want %d", w.Code, tc.want)
		}
		if (tc.want == 200) != strings.Contains(w.Body.String(), key) {
			t.Fatal("incorrect secret exposure")
		}
	}
}
