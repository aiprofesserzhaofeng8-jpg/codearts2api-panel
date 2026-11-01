package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsSaveFailureStillAppliesIdentityAndRateLimit(t *testing.T) {
	s := newTestServer(t, 0, 1)
	dir := filepath.Dir(s.Store.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(dir); err != nil {
			t.Error(err)
		}
		if err := os.Rename(dir+"-saved", dir); err != nil {
			t.Error(err)
		}
	})
	if err := os.WriteFile(dir, []byte("block state writes"), 0600); err != nil {
		t.Fatal(err)
	}
	postSettings(t, s, `{"plugin_name":"hot identity","rate_limit_enabled":true,"rate_limit_qps":5,"rate_limit_burst":10,"rate_limit_concurrency":1}`)
	if s.Up.Identity.PluginName != "hot identity" {
		t.Fatal("legacy failed Save skipped identity application")
	}
	release, _, ok := s.Limits.Acquire("isolated-key")
	if !ok {
		t.Fatal("first request was not admitted")
	}
	defer release()
	if second, _, ok := s.Limits.Acquire("isolated-key"); ok {
		second()
		t.Fatal("failed Save skipped rate-limit hot application")
	}
	w := httptest.NewRecorder()
	s.handleAdminSettings(w, httptest.NewRequest("POST", "/admin/api/settings", strings.NewReader(`{"plugin_name":"must not apply","pool":{"max_in_flight":0},"rate_limit_enabled":false}`)))
	if w.Code != 400 || s.Up.Identity.PluginName != "hot identity" {
		t.Fatal("invalid update changed runtime identity")
	}
	if second, _, ok := s.Limits.Acquire("isolated-key"); ok {
		second()
		t.Fatal("invalid update changed runtime limiter")
	}
}
