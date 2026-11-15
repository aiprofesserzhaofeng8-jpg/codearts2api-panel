package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsSaveFailurePreservesIdentityAndRateLimit(t *testing.T) {
	s := newTestServer(t, 0, 1)
	before := s.Store.Settings()
	identity := s.Up.Identity.PluginName
	dir := filepath.Dir(s.Store.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dir); _ = os.Rename(dir+"-saved", dir) })
	if err := os.WriteFile(dir, []byte("block state writes"), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleAdminSettings(w, httptest.NewRequest("POST", "/admin/api/settings", strings.NewReader(`{"plugin_name":"must not apply","rate_limit_enabled":true,"rate_limit_qps":5,"rate_limit_burst":10,"rate_limit_concurrency":1}`)))
	if w.Code != 500 || s.Up.Identity.PluginName != identity || s.Store.Settings().PluginName != before.PluginName || s.Store.Settings().RateLimitEnabled != before.RateLimitEnabled {
		t.Fatalf("failed save changed runtime or reported success: %d %s", w.Code, w.Body.String())
	}
	release, _, ok := s.Limits.Acquire("isolated-key")
	if !ok {
		t.Fatal("first request denied")
	}
	defer release()
	second, _, ok := s.Limits.Acquire("isolated-key")
	if !ok {
		t.Fatal("failed save applied limiter")
	}
	second()
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"-saved", dir); err != nil {
		t.Fatal(err)
	}
	// Restore cleanup's blocker arrangement after proving a successful retry.
	postSettings(t, s, `{"plugin_name":"saved identity","rate_limit_enabled":true,"rate_limit_qps":5,"rate_limit_burst":10,"rate_limit_concurrency":1}`)
	if s.Up.Identity.PluginName != "saved identity" {
		t.Fatal("committed setting not applied")
	}
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("cleanup"), 0600); err != nil {
		t.Fatal(err)
	}
}
