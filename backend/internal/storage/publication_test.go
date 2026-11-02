package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"codearts2api/internal/codearts"
)

func TestPermissionPublicationAndOutstandingLease(t *testing.T) {
	s := testPool(t, 1)
	sources := []string{"deepseek-v4.1-flash"}
	g, err := s.SaveGroup(AccountGroup{Name: "publication", AccountIDs: []string{"0"}, ModelMappings: []ModelMapping{{Name: "deepseek-4.1-flash", Sources: sources}}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddKeyInGroupAndSave("client", g.ID)
	if err != nil {
		t.Fatal(err)
	}

	// A pending durable mutation must not publish a second permission snapshot.
	s.saveMu.Lock()
	done := make(chan error, 1)
	revoked := cloneGroup(g)
	revoked.ModelMappings = nil
	revoked.AccountIDs = nil
	go func() { _, err := s.SaveGroup(revoked); done <- err }()
	a, _, release, err := s.AcquireMappedAccount("session", "deepseek-4.1-flash", sources, nil, k.Key)
	s.saveMu.Unlock()
	if err != nil || a == nil {
		t.Fatal("permission changed before transaction", err)
	}
	defer release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if a, _, r, err := s.AcquireMappedAccount("session", "deepseek-4.1-flash", sources, nil, k.Key); a != nil || err == nil {
		r()
		t.Fatal("stale request plan survived revocation")
	}
	if ok, err := s.RemoveAccountAndSave("0"); !ok || err != nil {
		t.Fatal(err)
	}
	release()
	release() // Release remains idempotent after revocation and deletion.
	s.mu.RLock()
	remaining := s.pool.inFlight["0"]
	s.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("lease leaked: %d", remaining)
	}
}

func TestSnapshotsAndFailedPublication(t *testing.T) {
	s := testPool(t, 1)
	s.UpdateSettings(func(c *Settings) { c.ModelAliases = map[string]string{"alias": "deepseek-v4.1-flash"} })
	settings := s.Settings()
	settings.ModelAliases["alias"] = "changed"
	if s.Settings().ModelAliases["alias"] != "deepseek-v4.1-flash" {
		t.Fatal("settings alias escaped")
	}
	g, err := s.SaveGroup(AccountGroup{Name: "snapshot", AccountIDs: []string{"0"}, AllowlistEnabled: true, ModelIDs: []string{"deepseek-v4.1-flash"}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddKeyInGroupAndSave("client", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := s.AccessScope(k.Key)
	scope.AccountIDs["0"] = false
	scope.ModelIDs["deepseek-v4.1-flash"] = false
	groups := s.Groups()
	groups[0].AccountIDs[0] = "other"
	groups[0].ModelIDs[0] = "other"
	keys := s.Keys()
	keys[0].Enabled = false
	original := s.path
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	denied := cloneGroup(g)
	denied.AccountIDs = nil
	if _, err := s.SaveGroup(denied); err == nil {
		t.Fatal("failed save reported success")
	}
	s.path = original
	current, err := s.AccessScope(k.Key)
	if err != nil || !current.AccountIDs["0"] || !current.ModelIDs["deepseek-v4.1-flash"] {
		t.Fatal("failed or external update published", err)
	}
	a, release, err := s.AcquireAccountForKey("", "deepseek-v4.1-flash", nil, k.Key)
	release()
	if a == nil || err != nil {
		t.Fatal("rollback not used by admission", err)
	}
	loaded, err := Open(original)
	if err != nil {
		t.Fatal(err)
	}
	current, err = loaded.AccessScope(k.Key)
	if err != nil || !current.AccountIDs["0"] {
		t.Fatal("rollback not durable", err)
	}
}

func TestRotatedNativeGenerationSurvivesPersistenceFailure(t *testing.T) {
	s := testPool(t, 1)
	before := codearts.StoredSession(s.Account("0"))
	rotated := &codearts.Session{AccessKey: "rotated", SecretKey: "fixture", RefreshToken: "new-generation"}
	original := s.path
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	if err := s.CommitAccountSession("0", before, rotated); err == nil {
		t.Fatal("failed persistence was hidden")
	}
	if codearts.StoredSession(s.Account("0")) != rotated {
		t.Fatal("rotation restored invalid old generation")
	}
	if err := s.CommitAccountSession("0", before, before); !errors.Is(err, codearts.ErrSessionChanged) {
		t.Fatal("stale generation accepted", err)
	}
	s.path = original
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(original)
	if err != nil || codearts.StoredSession(loaded.Account("0")).RefreshToken != "new-generation" {
		t.Fatal("new generation did not recover", err)
	}
	if codearts.StoredSession(loaded.Account("0")) == rotated {
		t.Fatal("reopen unexpectedly reused in-process identity")
	}
}
