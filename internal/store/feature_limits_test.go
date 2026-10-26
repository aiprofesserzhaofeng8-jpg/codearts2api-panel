package store

import (
	"codearts2api/internal/codearts"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelHealthIsolationAndRecovery(t *testing.T) {
	s := testPool(t, 1)
	now := time.Now()
	s.pool.now = func() time.Time { return now }
	s.MarkModelResult("0", "model-a", false, "limited", 60)
	if a, release := s.AcquireAccountForModel("conversation", "model-a", nil); a != nil {
		release()
		t.Fatal("cooling model selected")
	}
	if a, release := s.AcquireAccountForModel("conversation", "model-b", nil); a == nil {
		t.Fatal("unrelated model blocked")
	} else {
		release()
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ModelHealthSnapshot()["0"]["model-a"].Failures != 1 {
		t.Fatal("health not persisted")
	}
	now = now.Add(61 * time.Second)
	if a, release := s.AcquireAccountForModel("conversation", "model-a", nil); a == nil {
		t.Fatal("model never recovered")
	} else {
		release()
	}
	s.MarkModelResult("0", "model-a", true, "", 0)
	if s.ModelHealthSnapshot()["0"]["model-a"].Failures != 0 {
		t.Fatal("success not reset")
	}
}
func TestCredentialCommitRejectsNewLoginAndDeletion(t *testing.T) {
	s := testPool(t, 1)
	old := s.Account("0").Session
	newLogin := &codearts.Session{AccessKey: "new-login", SecretKey: "secret"}
	s.SetAccountSession("0", newLogin)
	if err := s.CommitAccountSession("0", old, &codearts.Session{AccessKey: "stale-refresh"}); !errors.Is(err, codearts.ErrSessionChanged) {
		t.Fatal("stale refresh overwrote login")
	}
	if s.Account("0").Session != newLogin {
		t.Fatal("login lost")
	}
	s.RemoveAccount("0")
	if err := s.CommitAccountSession("0", newLogin, old); !errors.Is(err, codearts.ErrSessionChanged) {
		t.Fatal("deleted account revived")
	}
}
func TestKeyBudgetReservationsResetAndMissingUsage(t *testing.T) {
	s := testPool(t, 0)
	key := s.AddKey("test").Key
	if err := s.SetKeyBudgetAndSave(key, KeyBudget{MaxRequests: 1}, false); err != nil {
		t.Fatal(err)
	}
	var admitted atomic.Int32
	var waiting, done sync.WaitGroup
	gate := make(chan struct{})
	for i := 0; i < 20; i++ {
		waiting.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			release, err := s.BeginKeyBudget(key)
			waiting.Done()
			if err != nil {
				return
			}
			admitted.Add(1)
			<-gate
			release()
			release()
		}()
	}
	waiting.Wait()
	if admitted.Load() > 1 {
		t.Fatal("request reservations raced")
	}
	close(gate)
	done.Wait()
	release, err := s.BeginKeyBudget(key)
	if err != nil {
		t.Fatal(err)
	}
	if s.SetKeyBudgetAndSave(key, KeyBudget{}, true) == nil {
		t.Fatal("reset allowed during active request")
	}
	release()
	if err := s.SetKeyBudgetAndSave(key, KeyBudget{MaxTokens: 100}, true); err != nil {
		t.Fatal(err)
	}
	s.Record(LogEntry{Status: 502, Attempts: 1, UsageIncomplete: true}, 0, key)
	if _, err := s.BeginKeyBudget(key); err == nil {
		t.Fatal("unknown token usage treated as free")
	}
	if err := s.SetKeyBudgetAndSave(key, KeyBudget{MaxUSD: 1}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckKeyBudgetProgress(key, "unpriced-model", nil); err == nil {
		t.Fatal("USD budget allowed unknown price")
	}
	if err := s.SetKeyBudgetAndSave(key, KeyBudget{}, true); err != nil {
		t.Fatal(err)
	}
	s.Record(LogEntry{Status: 200, Attempts: 1, Model: "unpriced-model", Usage: TokenUsage{Known: true, InputTokens: 12}}, 0, key)
	if err := s.SetKeyBudgetAndSave(key, KeyBudget{MaxUSD: 1}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginKeyBudget(key); err == nil {
		t.Fatal("unpriced historical consumption treated as zero")
	}
	s.ClearStats()
	s.ClearLogs()
	if s.Keys()[0].Budget.MaxUSD != 1 {
		t.Fatal("log reset removed budget")
	}
	for _, b := range []KeyBudget{{MaxRequests: -1}, {MaxTokens: -1}, {MaxUSD: math.Inf(1)}} {
		if s.SetKeyBudgetAndSave(key, b, false) == nil {
			t.Fatal("invalid budget accepted")
		}
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(s.Path())
	if err != nil || loaded.Keys()[0].Budget.MaxUSD != 1 {
		t.Fatal("budget not persisted")
	}
}
