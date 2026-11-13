package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/app"
	"codearts2api/internal/codearts"
	"codearts2api/internal/server"
	"codearts2api/internal/storage"
)

func managementFixture(t *testing.T) (*server.Server, *storage.Store, *codearts.Session) {
	t.Helper()
	st, err := storage.Open(filepath.Join(t.TempDir(), "data", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := codearts.ManualAKSKAccount("fixture", "fixture-ak", "fixture-sk")
	a.ID, a.MaxInFlight = "native", 2
	st.UpsertAccount(a)
	for _, platform := range []string{"workbuddy", "trae"} {
		st.UpsertAccount(&accounts.Account{ID: platform, Provider: platform, Scope: "cn", ExternalID: "fixture-user", Enabled: true, Credentials: []byte("{ \"opaque\": 9007199254740993 }")})
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	srv, err := app.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	return srv, st, codearts.StoredSession(a)
}

func blockManagementWrites(t *testing.T, st *storage.Store) func() {
	t.Helper()
	dir := filepath.Dir(st.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(dir+"-saved", dir); err != nil {
			t.Fatal(err)
		}
		restored = true
	}
	t.Cleanup(restore)
	return restore
}

func TestAccountManagementStrictFailureRetainsAccountAndCredentialCache(t *testing.T) {
	srv, st, session := managementFixture(t)
	srv.Cred.Remember("native", session)
	restore := blockManagementWrites(t, st)
	limit := 1
	for _, operation := range []func() error{
		func() error { return srv.AccountManagement.SetConcurrency("native", &limit) },
		func() error { return srv.AccountManagement.Remove("native") },
	} {
		var failure *accounts.ManagementError
		if err := operation(); !errors.As(err, &failure) || failure.Kind != accounts.ManagementInternal {
			t.Fatal("strict save failure was hidden", err)
		}
		if a := st.Account("native"); a == nil || a.MaxInFlight != 2 || codearts.StoredSession(a) != session || srv.Cred.Cached("native", time.Hour) != session {
			t.Fatal("failed transaction changed admission or invalidated credentials")
		}
	}
	restore()
	if err := srv.AccountManagement.Remove("native"); err != nil || st.Account("native") != nil || srv.Cred.Cached("native", time.Hour) != nil {
		t.Fatal("successful delete did not invalidate", err)
	}
	reopened, err := storage.Open(st.Path())
	if err != nil || reopened.Account("native") != nil || len(reopened.Accounts()) != 2 {
		t.Fatal("deletion was not durable or affected another platform", err)
	}
}

func TestAccountManagementSaveFailureRollsBack(t *testing.T) {
	srv, st, session := managementFixture(t)
	st.MarkModelResult("native", "test-model", false, "failure", 60)
	restore := blockManagementWrites(t, st)
	for _, action := range []func() error{
		func() error { return srv.AccountManagement.Toggle("native") },
		func() error { return srv.AccountManagement.SetPaused("native", true) },
		func() error { return srv.AccountManagement.ClearErrors("native") },
	} {
		if err := action(); err == nil {
			t.Fatal("save failure reported success")
		}
	}
	if !st.Account("native").Enabled || st.Account("native").Paused || len(st.ModelHealthSnapshot()["native"]) != 1 {
		t.Fatal("failed mutation changed live state")
	}
	srv.Cred.Remember("native", session)
	list := []*accounts.Account{{ID: "new-id", Enabled: true, NativeCredential: session}, {ID: "other", Provider: "workbuddy", ExternalID: "other-user", Enabled: true}}
	if _, err := srv.AccountManagement.Import(list); err == nil {
		t.Fatal("import reported success")
	}
	if st.Account("other") != nil || srv.Cred.Cached("native", time.Hour) == nil {
		t.Fatal("failed import changed account or cache")
	}
	restore()
	created, err := srv.AccountManagement.Import(list)
	if err != nil || len(created) != 2 || created[0] || !created[1] || list[0].ID != "native" {
		t.Fatal("deduplication changed", err)
	}
	if srv.Cred.Cached("native", time.Hour) != nil {
		t.Fatal("successful import did not invalidate cache")
	}
	if err := srv.AccountManagement.SetPaused("native", true); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(st.Path())
	if err != nil || !reopened.Account("native").Paused || reopened.Account("other") == nil {
		t.Fatal("successful mutation not durable", err)
	}
}

func TestAccountManagementExportPreservesOpaquePlatformBytes(t *testing.T) {
	srv, st, _ := managementFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, platform := range []string{"workbuddy", "trae"} {
		result, err := srv.AccountManagement.Export(ctx, platform)
		if err != nil || result.Native || result.JSON != string(st.Account(platform).Credentials) {
			t.Fatal("opaque export was normalized or sent to native resolution", err)
		}
	}
	result, err := srv.AccountManagement.Export(t.Context(), "native")
	if err != nil || !result.Native || result.HasRefresh {
		t.Fatal("permanent native export failed", err)
	}
	decoded, err := codearts.SessionFromJSON([]byte(result.JSON))
	if err != nil || decoded.AccessKey != "fixture-ak" || decoded.SecretKey != "fixture-sk" {
		t.Fatal("native credentials lost in export", err)
	}
	if err := srv.AccountManagement.Reimport(ctx, "native"); err == nil {
		t.Fatal("native reimport ignored cancellation")
	}
	if err := srv.AccountManagement.Reimport(t.Context(), "workbuddy"); err == nil {
		t.Fatal("non-CodeArts account entered native refresh")
	}
}

func TestAccountManagementNativeExportUsesCurrentSettingsAndReimportPolicy(t *testing.T) {
	srv, st, _, calls := credentialFixture(t, nil)
	st.UpdateSettings(func(s *storage.Settings) { s.AutoRefresh = false })
	if _, err := srv.AccountManagement.Export(t.Context(), "fixture"); err == nil || calls.Load() != 0 {
		t.Fatal("export ignored disabled renewal", err)
	}
	st.UpdateSettings(func(s *storage.Settings) { s.AutoRefresh = true })
	result, err := srv.AccountManagement.Export(t.Context(), "fixture")
	if err != nil || !result.Native || !result.HasRefresh || calls.Load() != 1 {
		t.Fatal("export did not follow current settings", err)
	}
	decoded, err := codearts.SessionFromJSON([]byte(result.JSON))
	if err != nil || decoded.RefreshToken != "fixture-rotated-token" || codearts.StoredSession(st.Account("fixture")).RefreshToken != decoded.RefreshToken {
		t.Fatal("export did not publish and encode the same generation", err)
	}
	srv2, st2, _, calls2 := credentialFixture(t, nil)
	st2.UpdateSettings(func(s *storage.Settings) { s.AutoRefresh = false })
	if err := srv2.AccountManagement.Reimport(t.Context(), "fixture"); err != nil || calls2.Load() != 1 {
		t.Fatal("explicit reimport lost its existing refresh policy", err)
	}
}
