package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExternalSearchGrantsAndSettlement(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{MaxUSD: 1, MaxRequests: 3})
	enableTestWallet(t, s, u, "2")
	initial, _ := s.User(u.ID)
	rev := func() uint64 { return s.SearchCatalog().Revision }
	add := func(name string) string {
		engine, key, on := "deepseek", "fixture-"+name, true
		id, err := s.SaveSearchService("", SearchServicePatch{Name: &name, Provider: &engine, APIKey: &key}, rev())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.SaveSearchService(id, SearchServicePatch{Enabled: &on}, rev()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := add("one"), add("two")
	if err := s.SetDefaultSearchService(b, rev()); err != nil {
		t.Fatal(err)
	}
	var g AccountGroup
	for _, group := range s.CanonicalGroups() {
		if group.ID == u.GroupID {
			g = group
		}
	}
	g.SearchServiceIDs = []string{a}
	g.AccountIDs = []string{}
	g, err := s.SaveUnifiedGroup(g, g.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BeginExternalSearch(k.Key, ""); err == nil {
		t.Fatal("unauthorized default accepted")
	}
	if err := s.SetDefaultSearchService(a, rev()); err != nil {
		t.Fatal(err)
	}
	for _, requested := range []string{a, ""} {
		service, release, err := s.BeginExternalSearch(k.Key, requested)
		if err != nil || service.ID != a || service.APIKey != "fixture-one" {
			t.Fatal("wrong service", err)
		}
		if err := s.Record(LogEntry{Time: time.Now().Unix(), Status: 200, RequestID: "fixture", Attempts: 1, UserID: u.ID, RequestKind: "search", SearchServiceID: a}, 0, k.Key); err != nil {
			t.Fatal(err)
		}
		release()
		release()
	}
	got, _ := s.User(u.ID)
	if got.Wallet.Balance != initial.Wallet.Balance {
		t.Fatal("wallet charged for external search")
	}
	if got.BudgetUsage.Requests != 2 || got.BudgetUsage.Incomplete || got.Wallet.ReviewRequired || got.BudgetUsage.USD != 0 {
		t.Fatal("search modified model accounting", got.BudgetUsage)
	}
	_, release, err := s.BeginExternalSearch(k.Key, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.BeginExternalSearch(k.Key, a); err == nil {
		t.Fatal("oversubscribed request budget")
	}
	release()
	if err = s.DeleteSearchService(a, rev()-1); err == nil {
		t.Fatal("stale deletion")
	}
	if err = s.DeleteSearchService(a, rev()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveUnifiedGroup(g, g.Revision); err == nil {
		t.Fatal("stale grant revived")
	}
	reopen, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	c := reopen.SearchCatalog()
	if c.DefaultID != "" || len(c.Services) != 1 || c.Services[0].ID != b {
		t.Fatal("incorrect catalog after restart")
	}
	for _, group := range reopen.CanonicalGroups() {
		if len(group.SearchServiceIDs) > 0 {
			t.Fatal("dangling group reference")
		}
	}
	if _, _, err = reopen.BeginExternalSearch(k.Key, ""); err == nil {
		t.Fatal("missing default allowed")
	}
}
func TestSearchServiceRollbackAndCredentials(t *testing.T) {
	s, _, _ := userFixture(t, KeyBudget{})
	name, engine, key := "first", "deepseek", "test-secret"
	rev := func() uint64 { return s.SearchCatalog().Revision }
	id, err := s.SaveSearchService("", SearchServicePatch{Name: &name, Provider: &engine, APIKey: &key}, rev())
	if err != nil {
		t.Fatal(err)
	}
	empty, on := "", true
	if _, err = s.SaveSearchService(id, SearchServicePatch{APIKey: &empty, Enabled: &on}, rev()); err != nil {
		t.Fatal(err)
	}
	if s.SearchCatalog().Services[0].APIKey != key {
		t.Fatal("blank patch erased key")
	}
	if _, err = s.SaveSearchService(id, SearchServicePatch{ClearKey: true}, rev()); err == nil {
		t.Fatal("cleared enabled key")
	}
	newer := "new-test-secret"
	if _, err = s.SaveSearchService(id, SearchServicePatch{APIKey: &newer}, rev()); err != nil {
		t.Fatal(err)
	}
	before := rev()
	original := s.path
	s.path = filepath.Join(t.TempDir(), "file", "bad")
	if err = os.WriteFile(filepath.Dir(s.path), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteSearchService(id, before); err == nil || rev() != before || s.SearchCatalog().Services[0].APIKey != newer {
		t.Fatal("failed transaction mutated catalog")
	}
	s.path = original
	off := false
	if _, err = s.SaveSearchService(id, SearchServicePatch{Enabled: &off, ClearKey: true}, rev()); err != nil {
		t.Fatal(err)
	}
	if s.SearchCatalog().Services[0].APIKey != "" {
		t.Fatal("secret not erased")
	}
	unsupported := "tavily"
	if _, err = s.SaveSearchService("", SearchServicePatch{Name: &name, Provider: &unsupported}, rev()); err == nil || strings.Contains(err.Error(), newer) {
		t.Fatal("invalid provider accepted")
	}
}
