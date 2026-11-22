package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func TestAdminKeySaveFailurePreservesAccess(t *testing.T) {
	s := newAdminTestServer(t, "a")
	g, err := s.Store.SaveGroup(storage.AccountGroup{Name: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.AccessManagement.AddKey("fixture", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(s.Store.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dir); _ = os.Rename(dir+"-saved", dir) })
	if err := os.WriteFile(dir, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"key", "toggle"} {
		w := adminProbeRequest(t, s, http.MethodDelete, "/admin/api/keys?"+action+"="+url.QueryEscape(access.KeyReference(key.Key)), "")
		if w.Code != 500 || !s.Store.ValidKey(key.Key) {
			t.Fatalf("failed %s reported success or changed access: %d", action, w.Code)
		}
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"-saved", dir); err != nil {
		t.Fatal(err)
	}
	w := adminProbeRequest(t, s, http.MethodDelete, "/admin/api/keys?toggle="+url.QueryEscape(access.KeyReference(key.Key)), "")
	reopened, err := storage.Open(s.Store.Path())
	if w.Code != 200 || err != nil || reopened.ValidKey(key.Key) {
		t.Fatal("successful toggle not durable", w.Code, err)
	}
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("cleanup"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAdminResetAndProbeSaveFailures(t *testing.T) {
	s := newAdminTestServer(t, "native")
	adapter := &checkinAdapter{}
	s.Providers = provider.NewRegistry()
	if err := s.Providers.Register(adapter); err != nil {
		t.Fatal(err)
	}
	s.Store.UpsertAccount(&accounts.Account{ID: "external", Provider: provider.WorkBuddy, Scope: "cn", ExternalID: "fixture"})
	s.Store.Record(storage.LogEntry{Status: 200}, 0)
	stats := s.Store.Stats()
	dir := filepath.Dir(s.Store.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dir); _ = os.Rename(dir+"-saved", dir) })
	if err := os.WriteFile(dir, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/api/logs", "/admin/api/stats"} {
		w := adminProbeRequest(t, s, http.MethodDelete, path, "")
		if w.Code != 500 || len(s.Store.Logs(0)) != 1 || s.Store.Stats() != stats {
			t.Fatal("reset failure hidden", path, w.Code)
		}
	}
	w := adminProbeRequest(t, s, http.MethodPost, "/admin/api/checkin", `{"id":"external"}`)
	if w.Code != 500 {
		t.Fatal("probe save failure hidden", w.Code)
	}
}
