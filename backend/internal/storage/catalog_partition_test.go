package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

func TestPlatformCatalogSurvivesReopenAndLastAccountDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	open := func() *Store {
		t.Helper()
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	a := &accounts.Account{ID: "first", Provider: "trae", Scope: "cn", Enabled: true}
	b := &accounts.Account{ID: "second", Provider: "trae", Scope: "cn", Enabled: true}
	s.UpsertAccount(a)
	s.UpsertAccount(b)
	models := make([]provider.Model, 129)
	for i := range models {
		models[i] = provider.Model{ID: fmt.Sprintf("model-%d", i), CatalogKind: "live", ClientReference: &provider.ClientReference{ContextWindow: 32768}}
	}
	at := time.Now().UTC()
	if err := s.SaveCatalogPartition(a, models, at); err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		s = open()
		n := 0
		s.RestoreCatalogPartitions(func(p, scope string, ms []provider.Model, updated time.Time) {
			if p != "trae" || scope != "cn" || !at.Equal(updated) {
				t.Fatal(p, scope, updated)
			}
			for i, m := range ms {
				if m.ID != models[i].ID || m.CatalogKind != "live" || m.ClientReference == nil || m.ClientReference.ContextWindow != 32768 {
					t.Fatal(i, m)
				}
			}
			n += len(ms)
		})
		if n != want {
			t.Fatalf("restored %d models, want %d", n, want)
		}
	}
	check(129)
	for i, a := range []*accounts.Account{a, b} {
		if ok, err := s.RemoveAccountAndSave(a.ID); !ok || err != nil {
			t.Fatal(ok, err)
		}
		check(129 * (1 - i))
	}
}

func TestCatalogRejectsRecreatedAccountAndRollsBackWriteError(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := &accounts.Account{ID: "a", Provider: "trae", Scope: "cn", Enabled: true}
	s.UpsertAccount(a)
	old := s.Account(a.ID)
	if _, err = s.RemoveAccountAndSave(a.ID); err != nil {
		t.Fatal(err)
	}
	s.UpsertAccount(a)
	if err = s.SaveCatalogPartition(old, nil, time.Now()); err == nil {
		t.Fatal("accepted deleted account response")
	}
	at := time.Now().UTC()
	if err = s.SaveCatalogPartition(a, []provider.Model{{ID: "kept"}}, at); err != nil {
		t.Fatal(err)
	}
	path := s.path
	s.path = filepath.Join(path, "unwritable")
	if err = s.SaveCatalogPartition(a, nil, time.Now()); err == nil {
		t.Fatal("expected write failure")
	}
	s.path = path
	for _, open := range []bool{false, true} {
		if open {
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
		}
		count, updated := s.CatalogPartitionStatus("trae", "cn")
		if count != 1 || updated == nil || !updated.Equal(at) {
			t.Fatal(open, count, updated)
		}
	}
}
