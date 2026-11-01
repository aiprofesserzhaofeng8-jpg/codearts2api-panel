package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGroupScopeNeverFallsBackOutsideMembers(t *testing.T) {
	s := testPool(t, 2)
	g, err := s.SaveGroup(AccountGroup{Name: "one", AccountIDs: []string{"0"}, AllowlistEnabled: true, ModelIDs: []string{"test"}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddKeyInGroupAndSave("client", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, release, err := s.AcquireAccountForKey("sticky", "test", nil, k.Key)
	if err != nil || a == nil || a.ID != "0" {
		t.Fatal("wrong group account", err)
	}
	defer release()
	b, release2, err := s.AcquireAccountForKey("sticky", "test", nil, k.Key)
	if err != nil || b == nil || b.ID != "0" {
		t.Fatal("wrong second lease", err)
	}
	defer release2()
	if a, r, e := s.AcquireAccountForKey("sticky", "test", nil, k.Key); a != nil || e != nil {
		r()
		t.Fatal("full group leaked to outside account", e)
	}
	if a, r, e := s.AcquireAccountForKey("", "other", nil, k.Key); a != nil || e == nil {
		r()
		t.Fatal("whitelist bypass")
	}
	g.AccountIDs = []string{}
	if _, err = s.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	if a, r, e := s.AcquireAccountForKey("sticky", "test", nil, k.Key); a != nil || e != nil {
		r()
		t.Fatal("empty group fell back")
	}
	if err = s.DeleteGroup(g.ID); err == nil {
		t.Fatal("bound group deleted")
	}
	if err = s.SetKeyGroupAndSave(k.Key, ""); err != nil {
		t.Fatal(err)
	}
	if a, r, e := s.AcquireAccountForKey("", "other", nil, k.Key); e != nil || a == nil || a.ID != "1" {
		r()
		t.Fatal("explicit ungroup did not restore legacy routing", e)
	} else {
		r()
	}
	if err = s.DeleteGroup(g.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGroupPersistenceAndFailedWritesRollBack(t *testing.T) {
	s := testPool(t, 1)
	g, err := s.SaveGroup(AccountGroup{Name: "test", AccountIDs: []string{"0", "0"}, AllowlistEnabled: true, ModelIDs: []string{"one", "one"}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddKeyInGroupAndSave("key", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups()) != 1 || len(r.Groups()[0].AccountIDs) != 1 || len(r.Groups()[0].ModelIDs) != 1 || r.Keys()[0].GroupID != g.ID {
		t.Fatal("group state not persisted/deduplicated")
	}
	snapshot := r.Groups()
	snapshot[0].AccountIDs[0] = "outside"
	if r.Groups()[0].AccountIDs[0] != "0" {
		t.Fatal("mutable group snapshot")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	g.AccountIDs = []string{}
	if _, err = s.SaveGroup(g); err == nil {
		t.Fatal("failed group save accepted")
	}
	if len(s.Groups()[0].AccountIDs) != 1 {
		t.Fatal("failed write changed live scope")
	}
	if err = s.SetKeyGroupAndSave(k.Key, ""); err == nil || s.Keys()[0].GroupID != g.ID {
		t.Fatal("failed key write changed binding")
	}
	if _, err = s.AddKeyInGroupAndSave("new", ""); err == nil || len(s.Keys()) != 1 {
		t.Fatal("failed new key remained active")
	}
}

func TestMissingGroupAndEmptyWhitelistDeny(t *testing.T) {
	s := testPool(t, 1)
	g, err := s.SaveGroup(AccountGroup{Name: "deny", AccountIDs: []string{"0"}, AllowlistEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddKeyInGroupAndSave("key", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.AccessScope(k.Key)
	if err != nil || p.AllowsModel("anything") {
		t.Fatal("empty whitelist allowed a model")
	}
	s.mu.Lock()
	s.st.Groups = nil
	s.mu.Unlock()
	if _, err = s.AccessScope(k.Key); err == nil {
		t.Fatal("missing group became unrestricted")
	}
	if err = s.SetKeyGroupAndSave(k.Key, "missing"); err == nil {
		t.Fatal("unknown binding accepted")
	}
	if _, err = s.AddKeyInGroupAndSave("new", "missing"); err == nil {
		t.Fatal("unknown group created unrestricted key")
	}
}
