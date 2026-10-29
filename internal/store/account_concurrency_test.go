package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"codearts2api/internal/codearts"
)

func TestPerAccountConcurrencyAcrossProviders(t *testing.T) {
	for _, upstream := range []string{"codearts", "workbuddy", "trae"} {
		t.Run(upstream, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			model, scope := "test-model", ""
			if upstream != "codearts" {
				model, scope = upstream+"/cn/test-model", "cn"
			}
			for _, id := range []string{"a", "b"} {
				s.UpsertAccount(&codearts.Account{ID: id, Provider: upstream, Scope: scope, ExternalID: id, Enabled: true})
			}
			for id, limit := range map[string]int{"a": 1, "b": 3} {
				if ok, err := s.SetAccountConcurrencyAndSave(id, limit); !ok || err != nil {
					t.Fatal(id, err)
				}
			}
			// Sticky reuse must not bypass the account's independent hard limit.
			a, releaseFirst := s.AcquireAccountForModel("sticky", model, nil)
			if a == nil || a.ID != "a" {
				t.Fatal("unexpected initial account")
			}
			defer releaseFirst()
			b, releaseSecond := s.AcquireAccountForModel("sticky", model, nil)
			if b == nil || b.ID != "b" {
				t.Fatal("full sticky account was reused")
			}
			defer releaseSecond()
			var wg sync.WaitGroup
			var mu sync.Mutex
			counts := map[string]int{"a": 1, "b": 1}
			var releases []func()
			for i := 0; i < 60; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					a, release := s.AcquireAccountForModel("", model, nil)
					if a != nil {
						mu.Lock()
						counts[a.ID]++
						releases = append(releases, release)
						mu.Unlock()
					} else {
						release()
					}
				}()
			}
			wg.Wait()
			if counts["a"] != 1 || counts["b"] != 3 {
				t.Fatalf("independent limits exceeded: %v", counts)
			}
			for id, want := range map[string]int{"a": 1, "b": 3} {
				v := s.PoolSnapshot()[id]
				if v.MaxInFlight != want || v.InFlight != want || v.State != "busy" {
					t.Fatalf("wrong live limit: %+v", v)
				}
			}
			for _, release := range releases {
				release()
				release()
			}
			releaseFirst()
			releaseSecond()
			for _, v := range s.PoolSnapshot() {
				if v.InFlight != 0 {
					t.Fatal("lease leaked")
				}
			}
		})
	}
}

func TestAccountConcurrencyChangesApplyWithoutCancellingInflight(t *testing.T) {
	s := testPool(t, 1)
	_, release1 := s.AcquireAccount("", nil)
	defer release1()
	_, release2 := s.AcquireAccount("", nil)
	defer release2()
	if _, err := s.SetAccountConcurrencyAndSave("0", 1); err != nil {
		t.Fatal(err)
	}
	if v := s.PoolSnapshot()["0"]; v.InFlight != 2 || v.MaxInFlight != 1 {
		t.Fatal("active leases changed", v)
	}
	if a, release := s.AcquireAccount("", nil); a != nil {
		release()
		t.Fatal("new request admitted above reduced limit")
	}
	release1()
	if a, release := s.AcquireAccount("", nil); a != nil {
		release()
		t.Fatal("new request admitted at limit")
	}
	release2()
	if a, release := s.AcquireAccount("", nil); a == nil {
		t.Fatal("slot did not recover")
	} else {
		release()
	}
	if _, err := s.SetAccountConcurrencyAndSave("0", 0); err != nil {
		t.Fatal(err)
	}
	s.UpdateSettings(func(c *Settings) { c.Pool.MaxInFlight = 4 })
	if v := s.PoolSnapshot()["0"]; v.MaxInFlight != 4 {
		t.Fatal("default inheritance failed", v)
	}
}

func TestAccountConcurrencyPersistsAndSurvivesCredentialReplacement(t *testing.T) {
	s := testPool(t, 1)
	if _, err := s.SetAccountConcurrencyAndSave("0", 3); err != nil {
		t.Fatal(err)
	}
	a := s.Account("0")
	a.ID = "replacement"
	a.MaxInFlight = 0
	if _, err := s.UpsertAccountAndSave(a, true); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Account("0").MaxInFlight != 3 || reopened.PoolSnapshot()["0"].MaxInFlight != 3 {
		t.Fatal("override lost after replacement/restart")
	}
	for _, limit := range []int{-1, 33} {
		if _, err := s.SetAccountConcurrencyAndSave("0", limit); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if found, err := s.SetAccountConcurrencyAndSave("missing", 2); found || err != nil {
		t.Fatal("missing account accepted")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	if _, err := s.SetAccountConcurrencyAndSave("0", 1); err == nil {
		t.Fatal("failed write reported success")
	}
	if s.Account("0").MaxInFlight != 3 || s.PoolSnapshot()["0"].MaxInFlight != 3 {
		t.Fatal("failed write changed live admission")
	}
}
