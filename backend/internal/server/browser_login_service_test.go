package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
	"codearts2api/internal/storage"
	codeartsup "codearts2api/internal/upstream/codearts"
)

func browserAdmin(s *Server, route, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://localhost/admin/api/login/"+route, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func stagedBrowser(t *testing.T, s *Server, ak string) (id, preview string) {
	t.Helper()
	start, err := s.BrowserLogins.Start("", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Logins.Cancel(start.ID) })
	if err := s.Logins.Get(start.ID).StageSession(&codearts.Session{AccessKey: ak, SecretKey: "fixture-private", SecurityToken: "fixture-sts", DomainID: "fixture-domain", AccountLabel: "verified-label", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	result, err := s.BrowserLogins.Poll(start.ID)
	if err != nil || !result.Ready {
		t.Fatal("preview missing", err)
	}
	return start.ID, result.PreviewID
}
func browserConfirmBody(id, preview string, replace bool) string {
	raw, _ := json.Marshal(map[string]any{"session_id": id, "preview_id": preview, "replace_existing": replace})
	return string(raw)
}

func TestBrowserLoginSaveFailureRetryAndConcurrentReplay(t *testing.T) {
	s := poolServer(t, 0)
	var invalidations atomic.Int32
	s.BrowserLogins = accounts.NewBrowserLogins(s.Store, codeartsup.NewBrowserLogin(s.Logins, s.Up), codearts.RandomHex, func() int { return s.Store.Settings().LoginPort }, func(id string) { invalidations.Add(1); s.Cred.Invalidate(id) })
	id, preview := stagedBrowser(t, s, "fixture-first-ak")
	body := browserConfirmBody(id, preview, false)
	if bad := browserAdmin(s, "complete", browserConfirmBody(id, "changed-preview", false)); bad.Code != 409 || len(s.Store.Accounts()) != 0 {
		t.Fatal("changed preview saved")
	}
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
	if err := os.WriteFile(dir, []byte("block persistence"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := browserAdmin(s, "complete", body)
	if failed.Code != 409 || !strings.Contains(failed.Body.String(), "凭据保存失败") || len(s.Store.Accounts()) != 0 || invalidations.Load() != 0 {
		t.Fatalf("failed save committed: %d %s", failed.Code, failed.Body.String())
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"-saved", dir); err != nil {
		t.Fatal(err)
	}
	restored = true
	// Concurrent client confirmations must still share the native commit gate.
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			w := browserAdmin(s, "complete", body)
			if w.Code != 200 {
				t.Errorf("confirm=%d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	if len(s.Store.Accounts()) != 1 || invalidations.Load() != 1 {
		t.Fatal("confirmation replay saved/invalidate more than once")
	}
	old := s.Store.Accounts()[0]
	reloaded, err := storage.Open(s.Store.Path())
	if err != nil || len(reloaded.Accounts()) != 1 {
		t.Fatal("save not durable", err)
	}
	if s.BrowserLogins.Cancel(id) {
		t.Fatal("saved login cancelled")
	}
	id2, preview2 := stagedBrowser(t, s, "fixture-replacement-ak")
	if w := browserAdmin(s, "complete", browserConfirmBody(id2, preview2, false)); w.Code != 409 || !strings.Contains(w.Body.String(), accounts.ErrAccountExists.Error()) {
		t.Fatal("duplicate account silently replaced")
	}
	if w := browserAdmin(s, "complete", browserConfirmBody(id2, preview2, true)); w.Code != 200 {
		t.Fatal("explicit replacement failed", w.Body.String())
	}
	updated := s.Store.Accounts()[0]
	if len(s.Store.Accounts()) != 1 || updated.ID != old.ID || codearts.StoredSession(updated).AccessKey != "fixture-replacement-ak" || invalidations.Load() != 2 {
		t.Fatal("replacement lost account identity")
	}
	resolved, err := s.Cred.Session(t.Context(), updated, codearts.ResolveOptions{})
	if err != nil || resolved != codearts.StoredSession(updated) {
		t.Fatal("new login generation not used", err)
	}
	id3, preview3 := stagedBrowser(t, s, "fixture-cancel-ak")
	if !s.BrowserLogins.Cancel(id3) {
		t.Fatal("cancel failed")
	}
	if w := browserAdmin(s, "complete", browserConfirmBody(id3, preview3, true)); w.Code != 409 || invalidations.Load() != 2 {
		t.Fatal("cancelled login saved")
	}
	if _, err := s.Store.RemoveAccountAndSave(old.ID); err != nil {
		t.Fatal(err)
	}
	if w := browserAdmin(s, "complete", browserConfirmBody(id2, preview2, true)); w.Code != 410 || len(s.Store.Accounts()) != 0 || invalidations.Load() != 2 {
		t.Fatal("replay resurrected deleted account")
	}
}

func TestBrowserLoginHTTPBoundaries(t *testing.T) {
	s := poolServer(t, 0)
	for _, route := range []string{"start", "poll", "callback", "complete", "cancel"} {
		r := httptest.NewRequest("GET", "http://localhost/admin/api/login/"+route, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 405 {
			t.Fatalf("%s method=%d", route, w.Code)
		}
		s.Store.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = "fixture-password" })
		if w := browserAdmin(s, route, `{}`); w.Code != 401 {
			t.Fatalf("%s auth=%d", route, w.Code)
		}
		s.Store.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = "" })
	}
	for _, tc := range []struct {
		route, body string
		status      int
	}{
		{"poll", `{`, 400}, {"poll", `{"session_id":"missing"}`, 404},
		{"callback", `{"session_id":"missing","url":"fixture-code"}`, 404},
		{"callback", `{"session_id":"missing","input":"` + strings.Repeat("x", 1<<20) + `"}`, 400},
		{"complete", `{"session_id":"missing","preview_id":"fixture"}`, 404},
		{"complete", `{"session_id":"missing","preview_id":"` + strings.Repeat("x", 64<<10) + `"}`, 400},
		{"start", `{"portal_host":"file:///invalid"}`, 500},
	} {
		if w := browserAdmin(s, tc.route, tc.body); w.Code != tc.status {
			t.Fatalf("%s expected %d got %d: %s", tc.route, tc.status, w.Code, w.Body.String())
		}
	}
	if len(s.Store.Accounts()) != 0 {
		t.Fatal("invalid HTTP request saved account")
	}
}
