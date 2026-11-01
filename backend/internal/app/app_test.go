package app_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/app"
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
)

func TestAssemblyRestoresAndGuardsCredentialGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original := &codearts.Session{AccessKey: "original", SecretKey: "fixture", ExpiresAt: time.Now().Add(time.Hour)}
	st.UpsertAccount(&accounts.Account{ID: "fixture", Enabled: true, Session: original})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := app.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{provider.WorkBuddy, provider.Trae} {
		if _, ok := srv.Providers.Get(id); !ok {
			t.Fatalf("missing platform %s", id)
		}
	}
	if srv.Growth == nil || srv.Credits == nil || srv.Rewards == nil || srv.Catalog == nil || srv.Configuration == nil || srv.AccessManagement == nil || srv.AccountManagement == nil || srv.Cred.Client != srv.Up || srv.BrowserLogins == nil || srv.ProviderLogins == nil || srv.ProviderCredentials == nil || srv.CodeArts == nil || srv.Completions == nil || srv.Logins == nil || srv.Search == nil || srv.Reg == nil || srv.Limits == nil {
		t.Fatal("incomplete dependencies")
	}
	stale := st.Account("fixture")
	expected := codearts.StoredSession(stale)
	fresh := &codearts.Session{AccessKey: "fresh", SecretKey: "fixture", ExpiresAt: time.Now().Add(time.Hour)}
	if err := srv.Cred.Commit("fixture", expected, fresh); err != nil {
		t.Fatal(err)
	}
	got, err := srv.Cred.Session(context.Background(), stale, codearts.ResolveOptions{AllowRefresh: false})
	if err != nil || got != fresh {
		t.Fatal("manager did not prefer latest stored credentials", err)
	}
	if err := srv.Cred.Commit("fixture", expected, original); !errors.Is(err, accounts.ErrCredentialChanged) || err != codearts.ErrSessionChanged {
		t.Fatal("stale refresh overwrote newer login")
	}
	reloaded, err := store.Open(path)
	if err != nil || codearts.StoredSession(reloaded.Account("fixture")).AccessKey != "fresh" {
		t.Fatal("assembled commit did not persist refresh", err)
	}
	if ok, err := st.RemoveAccountAndSave("fixture"); !ok || err != nil {
		t.Fatal(err)
	}
	if err := srv.Cred.Commit("fixture", fresh, original); !errors.Is(err, accounts.ErrCredentialChanged) {
		t.Fatal("deleted account resurrected by refresh")
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatal("assembled HTTP handler is unavailable")
	}
}

func TestAssemblyRetainsRotatedCredentialOnSaveFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state-dir")
	st, err := store.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := &codearts.Session{AccessKey: "original", SecretKey: "fixture"}
	st.UpsertAccount(&accounts.Account{ID: "fixture", Session: original})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	srv, err := app.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("block writes"), 0600); err != nil {
		t.Fatal(err)
	}
	fresh := &codearts.Session{AccessKey: "rotated", SecretKey: "fixture", ExpiresAt: time.Now().Add(time.Hour)}
	if err := srv.Cred.Commit("fixture", original, fresh); err == nil {
		t.Fatal("save failure was hidden")
	}
	if codearts.StoredSession(st.Account("fixture")) != fresh {
		t.Fatal("save failure restored an invalidated credential")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"-saved", dir); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Open(st.Path())
	if err != nil || codearts.StoredSession(reloaded.Account("fixture")).AccessKey != "rotated" {
		t.Fatal("rotated credential was not recoverable", err)
	}
}

func TestAssemblyRequiresStore(t *testing.T) {
	if _, err := app.Build(nil); err == nil {
		t.Fatal("missing store accepted")
	}
}
