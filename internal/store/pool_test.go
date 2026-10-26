package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/codearts"
)

func testPool(t *testing.T, n int) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprint(i)
		s.UpsertAccount(&codearts.Account{ID: id, Name: id, Enabled: true, Session: &codearts.Session{AccessKey: "ak" + id, SecretKey: "sk"}})
	}
	return s
}

func TestPoolConcurrentLeasesAndRelease(t *testing.T) {
	s := testPool(t, 3)
	var accepted atomic.Int32
	var acquired, done sync.WaitGroup
	gate := make(chan struct{})
	for i := 0; i < 100; i++ {
		acquired.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			a, release := s.AcquireAccount("", nil)
			if a != nil {
				accepted.Add(1)
			}
			acquired.Done()
			<-gate
			release()
			release()
		}()
	}
	acquired.Wait()
	if accepted.Load() != 6 {
		t.Errorf("got %d leases, want 3 accounts × 2", accepted.Load())
	}
	for _, v := range s.PoolSnapshot() {
		if v.State != "busy" || v.InFlight != 2 {
			t.Errorf("incorrect leased state: %+v", v)
		}
	}
	close(gate)
	done.Wait()
	for _, v := range s.PoolSnapshot() {
		if v.InFlight != 0 {
			t.Fatalf("lease leaked: %+v", v)
		}
	}
}

func TestPoolStickyExpiryPauseAndCooldown(t *testing.T) {
	s := testPool(t, 2)
	now := time.Now()
	s.pool.now = func() time.Time { return now }
	a, release := s.AcquireAccount("conversation", nil)
	release()
	b, release := s.AcquireAccount("conversation", nil)
	release()
	if a.ID != b.ID {
		t.Fatal("session did not stick")
	}
	s.SetAccountPaused(a.ID, true)
	b, release = s.AcquireAccount("conversation", nil)
	release()
	if a.ID == b.ID {
		t.Fatal("paused account selected")
	}
	if !s.Account(a.ID).Enabled {
		t.Fatal("pause must not disable maintenance")
	}
	s.CooldownAccount(b.ID, "rate limited", 60)
	if got, release := s.AcquireAccount("conversation", nil); got != nil {
		release()
		t.Fatal("cooling fallback must be forbidden")
	}
	now = now.Add(61 * time.Second)
	b, release = s.AcquireAccount("conversation", nil)
	release()
	if b == nil {
		t.Fatal("cooldown did not expire")
	}
	s.SetAccountPaused(a.ID, false)
	now = now.Add(1801 * time.Second)
	c, release := s.AcquireAccount("conversation", nil)
	release()
	if c == nil || c.ID == b.ID {
		t.Fatal("expired sticky binding still preferred")
	}
}

func TestPoolStrategies(t *testing.T) {
	for _, strategy := range []string{"round_robin", "least_inflight", "sequential_exhaust"} {
		t.Run(strategy, func(t *testing.T) {
			s := testPool(t, 2)
			s.UpdateSettings(func(c *Settings) { c.Pool.Strategy = strategy })
			a, release := s.AcquireAccount("", nil)
			release()
			b, release := s.AcquireAccount("", nil)
			release()
			if (a.ID == b.ID) != (strategy == "sequential_exhaust") {
				t.Fatalf("incorrect strategy %s: %s then %s", strategy, a.ID, b.ID)
			}
			c, release := s.AcquireAccount("", map[string]bool{b.ID: true})
			release()
			if c == nil || c.ID == b.ID {
				t.Fatal("excluded account selected")
			}
		})
	}
}

func TestPoolLeastInFlightBalancesHeldRequests(t *testing.T) {
	s := testPool(t, 2)
	a, releaseA := s.AcquireAccount("", nil)
	defer releaseA()
	b, releaseB := s.AcquireAccount("", nil)
	releaseB()
	c, releaseC := s.AcquireAccount("", nil)
	defer releaseC()
	if b.ID != c.ID || a.ID == c.ID {
		t.Fatal("did not choose less busy account")
	}
}

func TestPoolBreakerPersistenceAndRecovery(t *testing.T) {
	s := testPool(t, 1)
	for i := 0; i < 3; i++ {
		s.MarkAccountResult("0", false, "network failed")
	}
	a, release := s.AcquireAccount("", nil)
	release()
	if a != nil {
		t.Fatal("circuit breaker did not open")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.PoolSnapshot()["0"].State != "cooling" {
		t.Fatal("cooldown lost after restart")
	}
	reopened.ClearAccountErrors("0")
	a, release = reopened.AcquireAccount("", nil)
	release()
	if a == nil {
		t.Fatal("manual recovery failed")
	}
}

func TestPoolDeletedAccountLeaseAndBoundedSessions(t *testing.T) {
	s := testPool(t, 1)
	a, release := s.AcquireAccount("first", nil)
	s.RemoveAccount(a.ID)
	release()
	release()
	if len(s.pool.inFlight) != 0 {
		t.Fatal("deleted account retained lease")
	}
	s.UpsertAccount(&codearts.Account{ID: "replacement", Enabled: true})
	for i := 0; i < 4200; i++ {
		_, release := s.AcquireAccount(fmt.Sprint(i), nil)
		release()
	}
	if len(s.pool.bindings) > 4096 {
		t.Fatal("session memory is unbounded")
	}
}

func TestPoolReloginDeduplicatesRotatedCredentials(t *testing.T) {
	s := testPool(t, 0)
	s.UpsertAccount(&codearts.Account{ID: "first", Enabled: true, Session: &codearts.Session{DomainID: "same-cloud-account", AccessKey: "old"}})
	s.SetAccountPaused("first", true)
	s.SetAccountEnabled("first", false)
	newLogin := &codearts.Account{ID: "second", Enabled: true, Session: &codearts.Session{DomainID: "same-cloud-account", AccessKey: "rotated"}}
	if s.UpsertAccount(newLogin) || len(s.Accounts()) != 1 || newLogin.ID != "first" {
		t.Fatal("relogin duplicated the same account")
	}
	if a := s.Account("first"); a.Enabled || !a.Paused || a.Session.AccessKey != "rotated" {
		t.Fatal("relogin lost operator settings or failed to update credentials")
	}
}
