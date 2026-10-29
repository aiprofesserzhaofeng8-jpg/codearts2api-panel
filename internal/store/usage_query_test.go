package store

import (
	"path/filepath"
	"testing"
)

func TestUsageDatePaginationAndIndependentRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateSettings(func(st *Settings) { st.LogKeep = 1 })
	for i := 0; i < 65; i++ {
		s.Record(LogEntry{Time: int64(100 + i), Status: 200, Model: "deepseek-v4.1-flash", Usage: TokenUsage{Known: true, InputTokens: 10, OutputTokens: 5}, LatencyMS: 100}, 0)
	}
	a := s.QueryUsage(UsageQuery{Start: 110, End: 150, Page: 1, PageSize: 20})
	b := s.QueryUsage(UsageQuery{Start: 110, End: 150, Page: 2, PageSize: 20})
	if a.Total != 40 || a.Pages != 2 || len(a.Logs) != 20 || a.Logs[0].Time != 149 || b.Logs[19].Time != 110 {
		t.Fatalf("wrong range/page: %+v %+v", a, b)
	}
	if a.Stats != b.Stats || a.Stats.InputTokens != 400 || a.Stats.TotalRequests != 40 || a.Stats.PricedRequests != 40 {
		t.Fatal("summary must include the entire filtered range")
	}
	last := s.QueryUsage(UsageQuery{Start: 110, End: 150, Page: 99999, PageSize: 20})
	if last.Page != 2 {
		t.Fatal("page not clamped")
	}
	empty := s.QueryUsage(UsageQuery{Start: 1000, Page: 2, PageSize: 20})
	if empty.Total != 0 || empty.Page != 1 || empty.Pages != 1 || len(empty.Logs) != 0 {
		t.Fatal("bad empty page")
	}
	s.ClearLogs()
	if s.QueryUsage(UsageQuery{}).Total != 65 {
		t.Fatal("clearing logs erased usage")
	}
	s.ClearStats()
	if s.QueryUsage(UsageQuery{}).Total != 65 {
		t.Fatal("resetting cumulative counters erased history")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.QueryUsage(UsageQuery{}).Total != 65 {
		t.Fatal("history not persisted")
	}
}
func TestUsageHistoryBootstrapsOnce(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.st.Logs = []LogEntry{{Time: 10, Status: 200}, {Time: 11, Status: 500}}
	s.ClearLogs()
	if got := s.QueryUsage(UsageQuery{}); got.Total != 2 || got.Stats.FailedRequests != 1 {
		t.Fatalf("legacy logs not imported: %+v", got)
	}
	s.Record(LogEntry{Time: 12, Status: 200}, 0)
	if s.QueryUsage(UsageQuery{}).Total != 3 {
		t.Fatal("duplicate history import")
	}
}

func TestUsageCombinedFiltersApplyBeforePagingAndAnalysis(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		s.Record(LogEntry{Time: int64(100 + i), Provider: "trae", Model: "target", Protocol: "responses", Status: 200, Err: "stream failed", Usage: TokenUsage{Known: true, InputTokens: 10}}, 0)
	}
	for _, e := range []LogEntry{
		{Provider: "workbuddy", Model: "target", Protocol: "responses", Status: 500},
		{Provider: "trae", Model: "other", Protocol: "responses", Status: 500},
		{Provider: "trae", Model: "target", Protocol: "messages", Status: 500},
		{Provider: "trae", Model: "target", Protocol: "responses", Status: 200},
	} {
		e.Time = 110
		s.Record(e, 0)
	}
	q := UsageQuery{Provider: "trae", Model: "target", Protocol: "responses", Status: "error", Start: 100, End: 200, Page: 2, PageSize: 20}
	got := s.QueryUsage(q)
	if got.Total != 30 || len(got.Logs) != 10 || got.Stats.FailedRequests != 30 || got.Stats.InputTokens != 300 || len(got.Analysis.Models) != 1 || got.Analysis.Models[0].Stats.TotalRequests != 30 {
		t.Fatalf("filters must constrain records and full-range analysis: %+v", got)
	}
	q.Status = "success"
	got = s.QueryUsage(q)
	if got.Total != 1 || got.Stats.FailedRequests != 0 {
		t.Fatalf("stream errors must not count as successful: %+v", got)
	}
}
