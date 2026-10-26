package store

import (
	"path/filepath"
	"testing"
)

func TestUsageTotalsPersistBeyondLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateSettings(func(st *Settings) { st.LogKeep = 1 })
	s.Record(LogEntry{Time: 100, Status: 200, LatencyMS: 1500, Usage: TokenUsage{Known: true, InputTokens: 100, OutputTokens: 20, CachedTokens: 70}}, 0)
	s.Record(LogEntry{Time: 101, Status: 502, LatencyMS: 500}, 0)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Stats()
	if got.TotalRequests != 2 || got.UsageRequests != 1 || got.InputTokens != 100 || got.OutputTokens != 20 || got.CachedTokens != 70 || got.TotalLatencyMS != 2000 || got.LatencySamples != 2 {
		t.Fatalf("lost persisted usage: %+v", got)
	}
	if len(loaded.Logs(0)) != 1 {
		t.Fatal("log retention not applied")
	}
	loaded.ClearLogs()
	if loaded.Stats() != got {
		t.Fatal("clearing logs erased cumulative usage")
	}
	loaded.ClearStats()
	if loaded.Stats().InputTokens != 0 || loaded.Stats().LatencySamples != 0 {
		t.Fatal("reset did not clear new counters")
	}
}
