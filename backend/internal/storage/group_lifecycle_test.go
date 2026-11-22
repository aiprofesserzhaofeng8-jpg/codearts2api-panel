package storage

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/codearts"
)

func TestDeleteGroupRevokesEntitlementsAndRestoresOnlyExplicitAssignment(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	group := s.Groups()[0]
	next, err := s.SaveGroup(AccountGroup{Name: "next", AccountIDs: []string{"1"}})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.AddKeyInGroupAndSave("admin", group.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddKeyInGroupAndSave("other", next.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AdjustWallet(u.ID, 0, 100000, true, false, "fixture"); err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePlan(access.Plan{Name: "plan", GroupID: group.ID, Enabled: true, Days: 10, Period: "day"})
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.GenerateRedemptions(access.Redemption{Kind: "subscription", PlanID: p.ID, ExpiresAt: time.Now().Add(time.Hour).Unix()}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Redeem(u.ID, codes[0]); err != nil {
		t.Fatal(err)
	}
	balance, err := s.GenerateRedemptions(access.Redemption{Kind: "balance", Amount: 5, ExpiresAt: time.Now().Add(time.Hour).Unix()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	beforeSub, beforeLedger, beforeCodes := s.UserSubscription(u.ID), s.UserLedger(u.ID), s.Redemptions()
	path := s.path
	s.path = filepath.Join(path, "not-a-directory")
	if err := s.DeleteUnifiedGroup(group.ID, group.Revision); err == nil {
		t.Fatal("accepted failed write")
	}
	s.path = path
	plans, _ := s.PlanCatalog()
	if !s.ValidKey(admin.Key) || !s.ValidKey(k.Key) || !plans[0].Enabled || !reflect.DeepEqual(beforeCodes, s.Redemptions()) || s.UserSubscription(u.ID) != beforeSub {
		t.Fatal("failed delete published partial changes")
	}
	if err := s.DeleteUnifiedGroup(group.ID, group.Revision); err != nil {
		t.Fatal(err)
	}
	s = reopenJournal(t, s)
	plans, _ = s.PlanCatalog()
	sub := s.UserSubscription(u.ID)
	wantSub := beforeSub
	wantSub.Disabled = true
	c := s.Redemptions()
	if plans[0].Enabled || plans[0].Delisted || sub != wantSub || c[0] != beforeCodes[0] || !c[1].Disabled || c[2].Disabled {
		t.Fatal("wrong entitlement state after restart")
	}
	if !reflect.DeepEqual(s.UserLedger(u.ID), beforeLedger) || !s.ValidKey(other.Key) || len(s.Accounts()) != 2 {
		t.Fatal("unrelated data lost")
	}
	for _, key := range []string{admin.Key, k.Key} {
		if _, err := s.AccessScope(key); !errors.Is(err, access.ErrGroupUnavailable) {
			t.Fatal("scope did not reject missing group", err)
		}
		if _, err := s.BeginKeyBudget(key); err == nil {
			t.Fatal("deleted group budget admitted")
		}
	}
	if user, groupName, err := s.UserProfile(u.ID); err != nil || groupName != "" || user.GroupID != group.ID {
		t.Fatal("profile unavailable or group lost")
	}
	if _, err := s.AddUserKeyAndSave(u.ID, "invalid"); !errors.Is(err, access.ErrGroupUnavailable) {
		t.Fatal(err)
	}
	if err := s.ManageSubscription(u.ID, "", "enable", 0); !errors.Is(err, access.ErrGroupUnavailable) {
		t.Fatal(err)
	}
	if err := s.Redeem(u.ID, codes[1]); err == nil {
		t.Fatal("orphaned plan code redeemed")
	}
	if err := s.Redeem(u.ID, balance[0]); err != nil {
		t.Fatal(err)
	}
	update := access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: next.ID}
	if _, err := s.SaveUser(update, ""); err == nil {
		t.Fatal("silently replaced disabled subscription")
	}
	if err := s.ManageSubscription(u.ID, "", "remove", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveUser(update, ""); err != nil {
		t.Fatal(err)
	}
	scope, err := s.AccessScope(k.Key)
	if err != nil || !scope.Restricted || !scope.AccountIDs["1"] || len(scope.AccountIDs) != 1 || s.ValidKey(admin.Key) {
		t.Fatal("reassignment restored wrong permissions", err)
	}
}

func TestDeletedGroupSettlesExistingLeaseAndDeniesRetry(t *testing.T) {
	s, user, key := userFixture(t, KeyBudget{})
	if err := s.AdjustWallet(user.ID, 0, access.CreditScale, true, false, "initial"); err != nil {
		t.Fatal(err)
	}
	release, err := s.BeginKeyBudget(key.Key)
	if err != nil {
		t.Fatal(err)
	}
	account, releaseLease, err := s.AcquireAccountForKey("", "journal-model", nil, key.Key)
	if err != nil || account == nil {
		t.Fatal("initial lease", err)
	}
	entry := journalEntry("in-flight")
	entry.UserID = user.ID
	if err := s.ReserveConsumerQuote(key.Key, entry.RequestID, entry.Model, 128, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(user.GroupID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AcquireAccountForKey("", entry.Model, nil, key.Key); !errors.Is(err, access.ErrGroupUnavailable) {
		t.Fatal("retry acquired lease", err)
	}
	if err := s.ReserveConsumerQuote(key.Key, entry.RequestID, entry.Model, 128, nil); err == nil {
		t.Fatal("retry reserved new credit")
	}
	if err := s.Record(entry, 2, key.Key); err != nil {
		t.Fatal(err)
	}
	releaseLease()
	release()
	if s.UserReservedCredits(user.ID) != 0 || s.PoolSnapshot()[account.ID].InFlight != 0 {
		t.Fatal("hold or lease survived settlement")
	}
	s = reopenJournal(t, s)
	ledger := s.UserLedger(user.ID)
	if len(ledger) != 2 || s.QueryUserUsage(user.ID, UsageQuery{}).Total != 1 {
		t.Fatal("in-flight usage lost or counted twice")
	}
	u, _ := s.User(user.ID)
	if u.Wallet.Balance >= access.CreditScale || u.Wallet.Balance <= 0 || u.BudgetUsage.Requests != 1 {
		t.Fatal("wallet or shared budget lost settlement")
	}
}

// seedLegacyKey represents an already issued, ungrouped key in a test snapshot.
func (s *Store) seedLegacyKey(name string) *APIKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := &APIKey{
		Key:       "sk-ca-" + codearts.RandomHex(24),
		Name:      name,
		Enabled:   true,
		CreatedAt: time.Now().Unix(),
	}
	s.st.Keys = append(s.st.Keys, k)
	snapshot := *k
	return &snapshot
}

func groupedTestKey(t testing.TB, s *Store, name string) *APIKey {
	t.Helper()
	g, err := s.SaveGroup(AccountGroup{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddKeyInGroupAndSave(name, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
