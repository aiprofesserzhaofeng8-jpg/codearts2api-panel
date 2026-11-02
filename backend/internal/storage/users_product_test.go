package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"codearts2api/internal/access"
)

func TestUserConcurrencyAcrossKeysAndIdempotentRelease(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	limit := 2
	var err error
	u, err = s.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, MaxConcurrency: &limit}, "")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := s.AddUserKeyAndSave(u.ID, "two")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var ready, done sync.WaitGroup
	gate := make(chan struct{})
	for i := 0; i < 40; i++ {
		ready.Add(1)
		done.Add(1)
		go func(i int) {
			defer done.Done()
			key := k.Key
			if i%2 == 0 {
				key = k2.Key
			}
			release, err := s.BeginKeyBudget(key)
			if err == nil {
				accepted.Add(1)
			} else {
				var limitErr *access.UserConcurrencyError
				if !errors.As(err, &limitErr) {
					t.Error(err)
				}
			}
			ready.Done()
			<-gate
			if release != nil {
				release()
				release()
			}
		}(i)
	}
	ready.Wait()
	if accepted.Load() != 2 || s.Users()[0].InFlight != 2 {
		t.Error("cross-key limit", accepted.Load(), s.Users()[0].InFlight)
	}
	bob, err := s.SaveUser(access.UserUpdate{Username: "bob", GroupID: u.GroupID, MaxConcurrency: &limit}, access.PasswordHashPrefix+"fixture")
	if err != nil {
		t.Fatal(err)
	}
	bkey, err := s.AddUserKeyAndSave(bob.ID, "other user")
	if err != nil {
		t.Fatal(err)
	}
	brelease, err := s.BeginKeyBudget(bkey.Key)
	if err != nil {
		t.Fatal("user limit affected another user", err)
	}
	brelease()
	// Reducing a live limit keeps existing calls, while blocking new admission.
	limit = 1
	if _, err = s.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, MaxConcurrency: &limit}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginKeyBudget(k.Key); err == nil {
		t.Error("lower limit bypassed")
	}
	if !s.DeleteKey(k2.Key) {
		t.Error("key missing")
	}
	close(gate)
	done.Wait()
	if s.Users()[0].InFlight != 0 || s.Users()[0].BudgetUsage.Requests != 0 {
		t.Fatal("slot leak or rejected calls charged", s.Users()[0])
	}
	release, err := s.BeginKeyBudget(k.Key)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if got := reopenJournal(t, s).Users()[0]; got.MaxConcurrency != 1 || got.InFlight != 0 {
		t.Fatal("persistence", got)
	}
}

func TestProfilePatchPreservesAccountingPermissionsAndRollback(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	entry := journalEntry("profile-accounting")
	entry.UserID = u.ID
	if err := s.Record(entry, 2, k.Key); err != nil {
		t.Fatal(err)
	}
	before, _ := s.User(u.ID)
	name := "  测试用户  "
	view, err := s.UpdateUserProfile(u.ID, before.Revision, access.ProfileUpdate{DisplayName: &name})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.User(u.ID)
	if view.DisplayName != "测试用户" || after.Username != before.Username || after.Revision != before.Revision || after.BudgetUsage != before.BudgetUsage || after.GroupID != before.GroupID || after.PasswordHash != before.PasswordHash {
		t.Fatal("patch overwrote account fields", after)
	}
	// A failed profile write must neither publish the draft nor undo accounting.
	path := s.path
	blocker := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocker, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	bad := "not persisted"
	if _, err = s.UpdateUserProfile(u.ID, after.Revision, access.ProfileUpdate{DisplayName: &bad}); err == nil {
		t.Fatal("write succeeded")
	}
	got, _ := s.User(u.ID)
	if !reflect.DeepEqual(got, after) {
		t.Fatal("rollback changed user")
	}
	s.path = path
	if _, err = s.UpdateUserProfile(u.ID, after.Revision-1, access.ProfileUpdate{DisplayName: &bad}); !errors.Is(err, access.ErrUserAuthentication) {
		t.Fatal("stale session patched profile", err)
	}
	if got = reopenJournal(t, s).st.Users[0]; got.DisplayName != "测试用户" || got.BudgetUsage != before.BudgetUsage {
		t.Fatal("profile or journal lost", got)
	}
}

func TestUserProductSchemaMigrationPreservesExistingUsers(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{MaxRequests: 10})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err = json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state["version"] = 8
	for _, user := range state["users"].([]any) {
		for _, name := range []string{"max_concurrency", "display_name", "avatar"} {
			delete(user.(map[string]any), name)
		}
	}
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(s.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	again, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.User(u.ID)
	if !ok || again.st.Version != 9 || got.MaxConcurrency != 0 || got.PasswordHash == "" || got.Budget != u.Budget || got.GroupID != u.GroupID || !again.ValidKey(k.Key) {
		t.Fatal("schema 8 migration changed access", got)
	}
	// Do not interpret invalid persisted limits as unlimited.
	again.st.Users[0].MaxConcurrency = -1
	if err = again.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(s.path); err == nil {
		t.Fatal("negative persisted limit accepted")
	}
}
