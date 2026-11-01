package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/store"
)

func TestBrowserTicketLoginPersistsAccountThroughAdminAPI(t *testing.T) {
	s := poolServer(t, 0)
	s.Up.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/snap-manager/v1/login/ticket" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body := fmt.Sprintf(`{"user_name":"login-fixture","domain_id":"fixture-domain","credential":{"access":"fixture-ak","secret":"fixture-sk","securitytoken":"fixture-token","expires_at":%q}}`, time.Now().Add(time.Hour).Format(time.RFC3339))
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	call := func(path string, body any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(string(raw)))
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s returned %d: %s", path, w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	start := call("/admin/api/login/start", map[string]any{})
	id := start["session_id"].(string)
	defer s.Logins.Cancel(id)
	call("/admin/api/login/callback", map[string]any{"session_id": id, "url": start["callback"].(string) + "?secret=fixture-secret"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, _, _, _ := s.Logins.Get(id).Snapshot()
		if status == codearts.LoginConfirmed {
			break
		}
		if status == codearts.LoginFailed || time.Now().After(deadline) {
			t.Fatalf("login did not complete: %s", status)
		}
		time.Sleep(time.Millisecond)
	}
	repeat := call("/admin/api/login/callback", map[string]any{"session_id": id, "url": start["callback"].(string) + "?secret=fixture-secret"})
	if repeat["status"] != "confirmed" {
		t.Fatal("duplicate callback did not report existing confirmed status")
	}
	var previewID string
	for i := 0; i < 2; i++ {
		result := call("/admin/api/login/poll", map[string]any{"session_id": id})
		if result["status"] != "confirmed" || result["has_refresh"] != false {
			t.Fatalf("incorrect ticket login status: %v", result["status"])
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "fixture-sk") || strings.Contains(string(raw), "fixture-token") {
			t.Fatal("poll exposed credentials")
		}
		previewID = result["preview_id"].(string)
	}
	if len(s.Store.Accounts()) != 0 {
		t.Fatal("poll saved account without confirmation")
	}
	for i := 0; i < 2; i++ {
		call("/admin/api/login/complete", map[string]any{"session_id": id, "preview_id": previewID})
	}
	reloaded, err := store.Open(s.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	accounts := reloaded.Accounts()
	if len(accounts) != 1 || accounts[0].Session.AccessKey != "fixture-ak" || accounts[0].Session.HasRefresh() {
		t.Fatal("login account missing or duplicated")
	}
	accountID := accounts[0].ID
	call("/admin/api/accounts", map[string]any{"action": "remove", "id": accountID})
	if len(s.Store.Accounts()) != 0 {
		t.Fatal("delete left account in memory")
	}
	reopened, err := store.Open(s.Store.Path())
	if err != nil || len(reopened.Accounts()) != 0 {
		t.Fatal("deleted account returned from disk")
	}
	call("/admin/api/login/poll", map[string]any{"session_id": id})
	if len(s.Store.Accounts()) != 0 {
		t.Fatal("poll resurrected deleted account")
	}
	raw, _ := json.Marshal(map[string]any{"session_id": id, "preview_id": previewID})
	r := httptest.NewRequest("POST", "http://localhost/admin/api/login/complete", strings.NewReader(string(raw)))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 410 || len(s.Store.Accounts()) != 0 {
		t.Fatal("old login replay resurrected deleted account")
	}
}
