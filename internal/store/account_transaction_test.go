package store

import (
	"os"
	"path/filepath"
	"testing"

	"codearts2api/internal/codearts"
)

func TestAccountDeletionRollbackOnPersistenceFailure(t *testing.T) {
	s := testPool(t, 1)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	original := s.path
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	if ok, err := s.RemoveAccountAndSave("0"); ok || err == nil {
		t.Fatal("deletion falsely succeeded")
	}
	if s.Account("0") == nil {
		t.Fatal("failed deletion removed in-memory account")
	}
	s.path = original
	if ok, err := s.RemoveAccountAndSave("0"); !ok || err != nil {
		t.Fatal("valid deletion failed", err)
	}
	loaded, err := Open(original)
	if err != nil || loaded.Account("0") != nil {
		t.Fatal("account survived restart")
	}
	if ok, err := s.RemoveAccountAndSave("missing"); ok || err != nil {
		t.Fatal("nonexistent account silently reported deleted")
	}
}

func TestAccountCommitRequiresDuplicateApprovalAndRollsBack(t *testing.T) {
	s := testPool(t, 0)
	a := &codearts.Account{ID: "first", Enabled: true, Session: &codearts.Session{DomainID: "domain", AccessKey: "first-ak"}}
	if _, err := s.UpsertAccountAndSave(a, false); err != nil {
		t.Fatal(err)
	}
	b := &codearts.Account{ID: "second", Enabled: true, Session: &codearts.Session{DomainID: "domain", AccessKey: "second-ak"}}
	if _, err := s.UpsertAccountAndSave(b, false); err != ErrAccountExists {
		t.Fatal("duplicate update not gated")
	}
	if s.Account("first").Session.AccessKey != "first-ak" {
		t.Fatal("duplicate request changed credentials")
	}
	if isNew, err := s.UpsertAccountAndSave(b, true); isNew || err != nil || len(s.Accounts()) != 1 {
		t.Fatal("approved update duplicated account")
	}
}
