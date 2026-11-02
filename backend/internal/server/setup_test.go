package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/storage"
)

func TestSetupOwnerOnlyAndApplicationIsolation(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.PrepareInstallation()
	if err != nil {
		t.Fatal(err)
	}
	completed := 0
	h := NewSetupHandler(access.NewInstallation(s, token), func() { completed++ })
	request := func(method, path, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1000"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/auth/login", "/admin/api/users", "/admin/api/state", "/user/api/profile", "/v1/chat/completions", "/health", "/register"} {
		if w := request("POST", path, `{}`, ""); w.Code != 503 {
			t.Fatal("normal runtime exposed", path, w.Code)
		}
	}
	if w := request("GET", "/setup", "", ""); w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" || strings.Contains(w.Body.String(), token) {
		t.Fatal("installer HTML/CSP/credential leak", w.Code)
	}
	if w := request("GET", "/setup/status", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"needs_setup":true`) || strings.Contains(w.Body.String(), token) {
		t.Fatal("pending status", w.Code)
	}
	if w := request("GET", "/setup/install", "", ""); w.Code != 405 {
		t.Fatal(w.Code)
	}
	body := func(token string) string {
		raw, _ := json.Marshal(map[string]string{"setup_token": token, "password": "owner-password-here", "confirm_password": "owner-password-here"})
		return string(raw)
	}
	if w := request("POST", "/setup/install", body(token), "https://evil.example"); w.Code != 403 {
		t.Fatal("CSRF accepted", w.Code)
	}
	if w := request("POST", "/setup/install", body("wrong"), ""); w.Code != 403 {
		t.Fatal("unknown owner accepted", w.Code)
	}
	if w := request("POST", "/setup/install", strings.TrimSuffix(body(token), "}")+`,"role":"admin"}`, ""); w.Code != 400 {
		t.Fatal("unknown parameter accepted", w.Code)
	}
	if w := request("POST", "/setup/install", body(token)+`{}`, ""); w.Code != 400 {
		t.Fatal("trailing JSON accepted", w.Code)
	}
	if w := request("POST", "/setup/install", body(token), "http://localhost"); w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatal("install unexpectedly logged in", w.Code)
	}
	if completed != 1 {
		t.Fatal("wrong completion count")
	}
	if w := request("POST", "/setup/install", body(token), ""); w.Code != 403 || completed != 1 {
		t.Fatal("reinstallation allowed", w.Code)
	}
	w := httptest.NewRecorder()
	installedSetupStatus(w, httptest.NewRequest(http.MethodGet, "http://localhost/setup/status", nil))
	if !strings.Contains(w.Body.String(), `"ready":true`) || !strings.Contains(w.Body.String(), `"needs_setup":false`) {
		t.Fatal("normal setup status")
	}
	w = httptest.NewRecorder()
	closedSetupInstall(w, httptest.NewRequest(http.MethodPost, "http://localhost/setup/install", strings.NewReader(body(token))))
	if w.Code != 403 {
		t.Fatal("normal server installation unlocked")
	}
}
