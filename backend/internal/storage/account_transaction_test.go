package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"codearts2api/internal/codearts"
)

func TestAccountDeletionRollbackOnPersistenceFailure(t *testing.T) {
	s := testPool(t, 2)
	for i, ids := range [][]string{{"0", "1"}, {"0"}, {"1"}} {
		if _, err := s.SaveUnifiedGroup(AccountGroup{Name: []string{"shared", "last", "unrelated"}[i], AccountIDs: ids}, 0); err != nil {
			t.Fatal(err)
		}
	}
	groups := s.CanonicalGroups()
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
	if !reflect.DeepEqual(groups, s.CanonicalGroups()) {
		t.Fatal("failed deletion changed group membership or revision")
	}
	s.path = original
	if ok, err := s.RemoveAccountAndSave("0"); !ok || err != nil {
		t.Fatal("valid deletion failed", err)
	}
	loaded, err := Open(original)
	if err != nil || loaded.Account("0") != nil {
		t.Fatal("account survived restart")
	}
	after := loaded.CanonicalGroups()
	if !slices.Equal(after[0].AccountIDs, []string{"1"}) || len(after[1].AccountIDs) != 0 || after[0].Revision != groups[0].Revision+1 || after[1].Revision != groups[1].Revision+1 || !reflect.DeepEqual(after[2], groups[2]) {
		t.Fatal("incorrect persisted group updates", after)
	}
	if _, err := s.SaveUnifiedGroup(groups[0], groups[0].Revision); err == nil {
		t.Fatal("stale editor restored membership")
	}
	if ok, err := s.RemoveAccountAndSave("missing"); ok || err != nil {
		t.Fatal("nonexistent account silently reported deleted")
	}
}

func TestAccountCommitRequiresDuplicateApprovalAndRollsBack(t *testing.T) {
	s := testPool(t, 0)
	a := &codearts.Account{ID: "first", Enabled: true, NativeCredential: &codearts.Session{DomainID: "domain", AccessKey: "first-ak"}}
	if _, err := s.UpsertAccountAndSave(a, false); err != nil {
		t.Fatal(err)
	}
	b := &codearts.Account{ID: "second", Enabled: true, NativeCredential: &codearts.Session{DomainID: "domain", AccessKey: "second-ak"}}
	if _, err := s.UpsertAccountAndSave(b, false); err != ErrAccountExists {
		t.Fatal("duplicate update not gated")
	}
	if codearts.StoredSession(s.Account("first")).AccessKey != "first-ak" {
		t.Fatal("duplicate request changed credentials")
	}
	if isNew, err := s.UpsertAccountAndSave(b, true); isNew || err != nil || len(s.Accounts()) != 1 {
		t.Fatal("approved update duplicated account")
	}
}
