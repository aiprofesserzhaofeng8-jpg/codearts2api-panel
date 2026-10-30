package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"
)

func journalStore(t testing.TB) (*Store, string) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	key := s.AddKey("journal test").Key
	if err := s.SetPriceAndSave("journal-model", ModelPrice{Currency: "USD", Input: 1, Output: 2, Cached: .5}); err != nil {
		t.Fatal(err)
	}
	return s, key
}

func journalEntry(id string) LogEntry {
	return LogEntry{RequestID: id, Time: 123, Model: "journal-model", Status: 200,
		Attempts: 1, OutputLen: 2, Usage: TokenUsage{Known: true, InputTokens: 12, OutputTokens: 4, CachedTokens: 3}}
}

func reopenJournal(t *testing.T, s *Store) *Store {
	t.Helper()
	again, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	return again
}

func assertSettlementEqual(t *testing.T, want, got *Store) {
	t.Helper()
	if want.Stats() != got.Stats() || !reflect.DeepEqual(want.Logs(0), got.Logs(0)) ||
		!reflect.DeepEqual(want.Keys(), got.Keys()) ||
		!reflect.DeepEqual(want.QueryUsage(UsageQuery{}), got.QueryUsage(UsageQuery{})) {
		t.Fatal("settlement, budget, logs, or independent usage history changed during recovery")
	}
}

func TestUsageJournalRecoversWithoutSnapshot(t *testing.T) {
	s, key := journalStore(t)
	if err := s.SetKeyBudgetAndSave(key, KeyBudget{MaxRequests: 1}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(journalEntry("first"), 2, key); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.usageJournalPath())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(key)) || !bytes.Contains(raw, []byte(usageKeyHash(key))) {
		t.Fatal("journal exposed the API key or lost the stable key reference")
	}
	info, err := os.Stat(s.usageJournalPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("journal must be private")
	}
	var disk State
	raw, _ = os.ReadFile(s.Path())
	if json.Unmarshal(raw, &disk) != nil || disk.Stats.TotalRequests != 0 {
		t.Fatal("fixture accidentally saved the completed request in the snapshot")
	}
	again := reopenJournal(t, s)
	assertSettlementEqual(t, s, again)
	if _, err := again.BeginKeyBudget(key); err == nil {
		t.Fatal("exhausted request budget was reset on recovery")
	}
	assertSettlementEqual(t, again, reopenJournal(t, again))
}

func TestUsageJournalKeepsCheckpointedBudgetResetAndCleanup(t *testing.T) {
	s, key := journalStore(t)
	if err := s.Record(journalEntry("before-reset"), 2, key); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKeyBudgetAndSave(key, KeyBudget{MaxTokens: 100}, true); err != nil {
		t.Fatal(err)
	}
	// This administrative transaction checkpoints usage but leaves the journal
	// prefix intact. Replaying that prefix must not undo the reset.
	again := reopenJournal(t, s)
	if u := again.Keys()[0].BudgetUsage; u != (BudgetUsage{}) {
		t.Fatalf("reset budget was rebilled: %+v", u)
	}
	if err := again.Record(journalEntry("after-reset"), 2, key); err != nil {
		t.Fatal(err)
	}
	again.ClearLogs()
	again.ClearStats()
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := reopenJournal(t, again)
	assertSettlementEqual(t, again, loaded)
	if loaded.Keys()[0].BudgetUsage.Requests != 1 || loaded.QueryUsage(UsageQuery{}).Total != 2 {
		t.Fatal("cleanup resurrected old budget usage or deleted independent history")
	}
}

func TestUsageJournalPreservesHistoricalPriceAndUnknownUsage(t *testing.T) {
	s, key := journalStore(t)
	if err := s.Record(journalEntry("old-price"), 2, key); err != nil {
		t.Fatal(err)
	}
	unknown := journalEntry("unknown-usage")
	unknown.Usage = TokenUsage{}
	unknown.UsageIncomplete = true
	if err := s.Record(unknown, 0, key); err != nil {
		t.Fatal(err)
	}
	// Change only the snapshot's price to prove recovery never reprices a
	// durable settlement, even if configuration differs after restoration.
	raw, _ := os.ReadFile(s.Path())
	var disk State
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	disk.Prices["journal-model"] = ModelPrice{Currency: "USD", Input: 900, Output: 900, Cached: 900}
	raw, _ = json.Marshal(disk)
	if err := os.WriteFile(s.Path(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	again := reopenJournal(t, s)
	assertSettlementEqual(t, s, again)
	if !again.Keys()[0].BudgetUsage.Incomplete {
		t.Fatal("unknown usage became a zero-cost receipt")
	}
}

func TestUsageJournalCompactionPreservesConcurrentSuffix(t *testing.T) {
	s, key := journalStore(t)
	if err := s.Record(journalEntry("snapshotted"), 2, key); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s.st)
	if err != nil {
		t.Fatal(err)
	}
	offset := s.journalSize
	// Deterministically model a settlement arriving during snapshot disk I/O.
	if err := s.Record(journalEntry("during-save"), 2, key); err != nil {
		t.Fatal(err)
	}
	s.saveMu.Lock()
	err = s.writeState(raw)
	s.saveMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s.journalMu.Lock()
	err = s.compactUsageJournal(offset)
	s.journalMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	tail, _ := os.ReadFile(s.usageJournalPath())
	if bytes.Contains(tail, []byte("snapshotted")) || !bytes.Contains(tail, []byte("during-save")) {
		t.Fatal("compaction lost a post-checkpoint settlement")
	}
	assertSettlementEqual(t, s, reopenJournal(t, s))
}

func TestUsageJournalSkipsAlreadyCommittedPrefix(t *testing.T) {
	s, key := journalStore(t)
	if err := s.Record(journalEntry("once"), 2, key); err != nil {
		t.Fatal(err)
	}
	prefix, _ := os.ReadFile(s.usageJournalPath())
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// Crash after snapshot rename but before journal compaction.
	if err := os.WriteFile(s.usageJournalPath(), prefix, 0o600); err != nil {
		t.Fatal(err)
	}
	assertSettlementEqual(t, s, reopenJournal(t, s))
}

func TestUsageJournalRepairsInterruptedTail(t *testing.T) {
	s, key := journalStore(t)
	if err := s.Record(journalEntry("complete"), 2, key); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(s.usageJournalPath(), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"sequence":2,"entry":`)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	again := reopenJournal(t, s)
	assertSettlementEqual(t, s, again)
	if err := again.Record(journalEntry("next"), 2, key); err != nil {
		t.Fatal(err)
	}
	assertSettlementEqual(t, again, reopenJournal(t, again))
}

func TestUsageJournalRejectsCorruptionAndMissingSnapshot(t *testing.T) {
	for _, tail := range []string{"{broken}\n", "{\"sequence\":3}\n", "{\"sequence\":0}\n", "null\n"} {
		t.Run(tail, func(t *testing.T) {
			s, key := journalStore(t)
			if err := s.Record(journalEntry("valid"), 2, key); err != nil {
				t.Fatal(err)
			}
			prefix, _ := os.ReadFile(s.usageJournalPath())
			raw := append(prefix, tail...)
			if err := os.WriteFile(s.usageJournalPath(), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(s.Path()); err == nil {
				t.Fatal("corruption silently admitted requests")
			}
			after, _ := os.ReadFile(s.usageJournalPath())
			if !bytes.Equal(raw, after) {
				t.Fatal("startup destroyed the corrupt evidence")
			}
		})
	}
	s, key := journalStore(t)
	if err := s.Record(journalEntry("orphan"), 2, key); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.Path()); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(s.Path()); err == nil {
		t.Fatal("orphaned journal was mistaken for a fresh installation")
	}
}

func TestUsageJournalFailureKeepsCountersAndStopsAdmission(t *testing.T) {
	s, key := journalStore(t)
	if err := os.Mkdir(s.usageJournalPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(journalEntry("write-failed"), 2, key); err == nil {
		t.Fatal("journal write failure was ignored")
	}
	if s.Stats().TotalRequests != 1 || s.Keys()[0].BudgetUsage.Tokens != 16 {
		t.Fatal("real consumption was removed from memory after a write failure")
	}
	if _, err := s.BeginKeyBudget(key); !errors.Is(err, ErrUsagePersistence) {
		t.Fatal("new requests can consume against unpersisted accounting")
	}
	if _, err := s.BeginKeyBudget(""); !errors.Is(err, ErrUsagePersistence) {
		t.Fatal("anonymous requests bypassed the persistence failure")
	}
	if err := s.CheckKeyBudgetProgress(key, "journal-model", nil); !errors.Is(err, ErrUsagePersistence) {
		t.Fatal("retry bypassed the persistence failure")
	}
	if err := s.Save(); err == nil {
		t.Fatal("an unusable journal incorrectly cleared the storage failure")
	}
	if err := os.Remove(s.usageJournalPath()); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	release, err := s.BeginKeyBudget(key)
	if err != nil {
		t.Fatal(err)
	}
	release()
	assertSettlementEqual(t, s, reopenJournal(t, s))
}

func TestUsageJournalConcurrentSaveAndSettlement(t *testing.T) {
	s, key := journalStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 220)
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				errs <- s.Record(journalEntry(fmt.Sprintf("%d-%d", worker, i)), 2, key)
			}
		}(worker)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			errs <- s.Save()
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if s.Stats().TotalRequests != 200 || s.Keys()[0].BudgetUsage.Requests != 200 {
		t.Fatal("concurrent settlement lost an update")
	}
	assertSettlementEqual(t, s, reopenJournal(t, s))
}

func BenchmarkUsageSettlement(b *testing.B) {
	for _, history := range []int{0, 10000} {
		b.Run(fmt.Sprintf("history_%d", history), func(b *testing.B) {
			s, key := journalStore(b)
			s.st.UsageRecords = make([]LogEntry, history, history+b.N+1)
			s.st.UsageHistoryInitialized = true
			entry := journalEntry("benchmark")
			timings := make([]int64, b.N)
			if err := s.Save(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				start := time.Now()
				s.Record(entry, 2, key)
				timings[i] = time.Since(start).Nanoseconds()
			}
			b.StopTimer()
			slices.Sort(timings)
			b.ReportMetric(float64(timings[(len(timings)-1)*95/100]), "p95-ns")
			b.ReportMetric(float64(timings[(len(timings)-1)*99/100]), "p99-ns")
			if _, err := s.BeginKeyBudget(key); err != nil {
				b.Fatal(err)
			}
		})
	}
}
