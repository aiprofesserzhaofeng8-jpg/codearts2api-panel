package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"codearts2api/internal/access"
)

func userFixture(t *testing.T, budget KeyBudget) (*Store, access.UserView, APIKey) {
	t.Helper()
	s := testPool(t, 2)
	g, err := s.SaveGroup(AccountGroup{Name: "user group", AccountIDs: []string{"0"}, AllowlistEnabled: true, ModelIDs: []string{"journal-model"}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.SaveUser(access.UserUpdate{Username: "alice", GroupID: g.ID, Budget: &budget}, access.PasswordHashPrefix+strings.Repeat("0", 32)+"$"+strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddUserKeyAndSave(u.ID, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetPriceAndSave("journal-model", ModelPrice{Currency: "USD", Input: 1, Output: 2, Cached: .5}); err != nil {
		t.Fatal(err)
	}
	return s, u, k
}
func TestUsersIsolationAndAtomicRevocation(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	admin := s.AddKey("legacy admin")
	if owner, ok := s.AuthenticateKey(k.Key); !ok || owner != u.ID {
		t.Fatal(owner, ok)
	}
	a, release, err := s.AcquireAccountForKey("same", "journal-model", nil, k.Key)
	if err != nil || a == nil || a.ID != "0" {
		t.Fatal(a, err)
	}
	if _, err = s.AccessScope(admin.Key); err != nil {
		t.Fatal(err)
	}
	if err = s.SetKeyGroupAndSave(k.Key, ""); err == nil {
		t.Fatal("individual key escaped user group")
	}
	if err = s.DeleteGroup(u.GroupID); err == nil {
		t.Fatal("referenced user group deleted")
	}
	if _, err = s.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: ""}, ""); err == nil {
		t.Fatal("unrestricted user allowed")
	}
	other, err := s.SaveUser(access.UserUpdate{Username: "bob", GroupID: u.GroupID}, access.PasswordHashPrefix+"fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.RevealUserKey(other.ID, access.KeyReference(k.Key)); ok {
		t.Fatal("cross-user reveal")
	}
	if ok, err := s.ChangeUserKeyAndSave(other.ID, access.KeyReference(k.Key), true); err != nil || ok {
		t.Fatal("cross-user delete", ok, err)
	}
	off := false
	if _, err = s.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, Enabled: &off}, ""); err != nil {
		t.Fatal(err)
	}
	if s.ValidKey(k.Key) {
		t.Fatal("disabled owner still authenticated")
	}
	if _, _, err = s.AcquireAccountForKey("same", "journal-model", nil, k.Key); err == nil {
		t.Fatal("disabled owner admitted")
	}
	if _, err = s.BeginKeyBudget(k.Key); err == nil {
		t.Fatal("disabled owner reserved budget")
	}
	release()
	release()
	if s.PoolSnapshot()["0"].InFlight != 0 {
		t.Fatal("outstanding lease not released")
	}
	if !s.ValidKey(admin.Key) {
		t.Fatal("legacy administrator key changed")
	}
}
func TestUsersGroupChangeAndFailedSaveAreAtomic(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	g, err := s.SaveGroup(AccountGroup{Name: "next", AccountIDs: []string{"1"}, AllowlistEnabled: true, ModelIDs: []string{"journal-model"}})
	if err != nil {
		t.Fatal(err)
	}
	before := s.Users()
	beforeKeys := s.Keys()
	oldPath := s.path
	blocker := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocker, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	if _, err = s.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: g.ID}, ""); err == nil {
		t.Fatal("write succeeded")
	}
	if !reflect.DeepEqual(before, s.Users()) || !reflect.DeepEqual(beforeKeys, s.Keys()) {
		t.Fatal("failed transaction published permissions")
	}
	if _, err = s.AddUserKeyAndSave(u.ID, "fail"); err == nil || len(s.Keys()) != 1 {
		t.Fatal("failed key create published")
	}
	if _, err = s.ChangeUserKeyAndSave(u.ID, access.KeyReference(k.Key), true); err == nil || len(s.Keys()) != 1 {
		t.Fatal("failed delete published")
	}
	s.path = oldPath
	if _, err = s.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: g.ID}, ""); err != nil {
		t.Fatal(err)
	}
	a, release, err := s.AcquireAccountForKey("same", "journal-model", nil, k.Key)
	if err != nil || a == nil || a.ID != "1" {
		t.Fatal(a, err)
	}
	release()
	again := reopenJournal(t, s)
	if again.UserKeys(u.ID)[0].GroupID != g.ID {
		t.Fatal("group assignment lost")
	}
}
func TestUsersSharedBudgetConcurrentKeysAndJournalRecovery(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{MaxRequests: 1})
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
			if i%2 != 0 {
				key = k2.Key
			}
			release, err := s.BeginKeyBudget(key)
			if err == nil {
				accepted.Add(1)
			}
			ready.Done()
			<-gate
			if err == nil {
				release()
				release()
			}
		}(i)
	}
	ready.Wait()
	if accepted.Load() != 1 {
		t.Errorf("accepted=%d", accepted.Load())
	}
	close(gate)
	done.Wait()
	release, err := s.BeginKeyBudget(k.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, ResetBudget: true}, ""); err == nil {
		t.Fatal("active user budget reset")
	}
	entry := journalEntry("shared")
	entry.UserID = u.ID
	if err = s.Record(entry, 2, k.Key); err != nil {
		t.Fatal(err)
	}
	release()
	if _, err = s.BeginKeyBudget(k2.Key); err == nil {
		t.Fatal("second key bypassed aggregate request budget")
	}
	again := reopenJournal(t, s)
	if again.Users()[0].BudgetUsage.Requests != 1 || again.QueryUserUsage(u.ID, UsageQuery{}).Total != 1 {
		t.Fatal("owner or shared budget lost on replay")
	}
	if _, err = again.BeginKeyBudget(k2.Key); err == nil {
		t.Fatal("restart reset aggregate budget")
	}
	if _, err = again.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, ResetBudget: true}, ""); err != nil {
		t.Fatal(err)
	}
	again = reopenJournal(t, again)
	if again.Users()[0].BudgetUsage.Requests != 0 || again.QueryUserUsage(u.ID, UsageQuery{}).Total != 1 {
		t.Fatal("reset changed history or replayed twice")
	}
}
func TestUsersDeletedKeyKeepsOwnershipAndPrivateUsageProjection(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	other, err := s.SaveUser(access.UserUpdate{Username: "bob", GroupID: u.GroupID}, access.PasswordHashPrefix+"fixture")
	if err != nil {
		t.Fatal(err)
	}
	entry := journalEntry("owned")
	entry.UserID = u.ID
	entry.RequestedModel = "public-4.1"
	entry.Account = "private-account"
	entry.Err = "token=private-upstream-error"
	entry.AttemptDetails = []RequestAttempt{{Account: "private-attempt"}}
	if !s.DeleteKey(k.Key) {
		t.Fatal("key missing")
	}
	if err = s.Save(); err != nil {
		t.Fatal(err)
	}
	if err = s.Record(entry, 2, k.Key); err != nil {
		t.Fatal(err)
	}
	if err = s.Record(journalEntry("admin-history"), 2); err != nil {
		t.Fatal(err)
	}
	page := s.QueryUserUsage(u.ID, UsageQuery{})
	if page.Total != 1 || page.Retained != 1 || page.Stats.TotalRequests != 1 || page.Logs[0].Model != "public-4.1" {
		t.Fatal(page)
	}
	raw, _ := json.Marshal(page)
	for _, secret := range []string{"private-account", "private-attempt", "private-upstream-error", k.Key, "admin-history"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("sensitive usage projection", secret)
		}
	}
	if s.QueryUserUsage(other.ID, UsageQuery{}).Total != 0 {
		t.Fatal("other user sees usage")
	}
	again := reopenJournal(t, s)
	if again.Users()[0].BudgetUsage.Requests != 1 || again.QueryUserUsage(u.ID, UsageQuery{}).Total != 1 {
		t.Fatal("deleted-key replay lost ownership")
	}
}
func TestUsersMeteredBudgetAndSearchFailClosed(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{MaxTokens: 100})
	k2, err := s.AddUserKeyAndSave(u.ID, "two")
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.BeginKeyBudget(k.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginKeyBudget(k2.Key); err == nil {
		t.Fatal("parallel user metered request")
	}
	release()
	if err = s.CheckSearchBudget(k2.Key); err == nil {
		t.Fatal("unmetered search bypassed user budget")
	}
	entry := journalEntry("unknown")
	entry.Usage = TokenUsage{}
	if err = s.Record(entry, 0, k.Key); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginKeyBudget(k2.Key); err == nil {
		t.Fatal("unknown usage silently counted as zero")
	}
	if _, err = s.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, ResetBudget: true, Budget: &KeyBudget{MaxUSD: 1}}, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckKeyBudgetProgress(k.Key, "unpriced", nil); err == nil {
		t.Fatal("unpriced model bypassed user USD budget")
	}
}

func BenchmarkUserAdmission(b *testing.B) {
	for _, n := range []int{1, 100, 1024} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			s, err := Open(filepath.Join(b.TempDir(), "state.json"))
			if err != nil {
				b.Fatal(err)
			}
			s.st.Groups = []AccountGroup{{ID: "g", Name: "g"}}
			for i := 0; i < n; i++ {
				id := strconv.Itoa(i)
				id += strings.Repeat("x", 26-len(id)) // Production rand.Text identity length.
				s.st.Users = append(s.st.Users, access.User{ID: id, GroupID: "g", Enabled: true})
			}
			key := s.AddKey("bench")
			s.st.Keys[0].UserID = s.st.Users[n-1].ID
			s.st.Keys[0].GroupID = "g"
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, ok := s.AuthenticateKey(key.Key); !ok {
					b.Fatal("auth")
				}
				release, err := s.BeginKeyBudget(key.Key)
				if err != nil {
					b.Fatal(err)
				}
				release()
			}
		})
	}
}

func TestUsersShareGlobalAccountLeaseLimit(t *testing.T) {
	s, alice, akey := userFixture(t, KeyBudget{})
	bob, err := s.SaveUser(access.UserUpdate{Username: "bob", GroupID: alice.GroupID}, access.PasswordHashPrefix+"fixture")
	if err != nil {
		t.Fatal(err)
	}
	bkey, err := s.AddUserKeyAndSave(bob.ID, "bob")
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
			key := akey.Key
			if i%2 != 0 {
				key = bkey.Key
			}
			account, release, err := s.AcquireAccountForKey("", "journal-model", nil, key)
			if err == nil && account != nil {
				accepted.Add(1)
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
	if got := accepted.Load(); got != 2 {
		t.Errorf("shared account concurrency exceeded: %d", got)
	}
	close(gate)
	done.Wait()
	if s.PoolSnapshot()["0"].InFlight != 0 {
		t.Fatal("user lease leaked")
	}
}
