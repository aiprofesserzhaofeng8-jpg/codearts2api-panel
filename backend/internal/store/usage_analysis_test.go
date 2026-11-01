package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestUsageAnalysisUsesFilteredRangeAcrossPages(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 45; i++ {
		provider, model := "workbuddy", "workbuddy/cn/test"
		if i%2 == 0 {
			provider, model = "trae", "trae/cn/test"
		}
		s.Record(LogEntry{Time: int64(100 + i), Provider: provider, Model: model, Protocol: "responses", Status: 200, Usage: TokenUsage{Known: true, InputTokens: 100, CachedTokens: 80, OutputTokens: 20}}, 0)
	}
	a := s.QueryUsage(UsageQuery{Provider: "workbuddy", Page: 1, PageSize: 10})
	b := s.QueryUsage(UsageQuery{Provider: "workbuddy", Page: 2, PageSize: 10})
	if !reflect.DeepEqual(a.Analysis, b.Analysis) || a.Total != 22 {
		t.Fatal("charts changed with pagination or ignored provider filter")
	}
	if len(a.Analysis.Models) != 1 || a.Analysis.Models[0].Stats.TotalRequests != 22 || a.Analysis.Models[0].Stats.InputTokens != 2200 {
		t.Fatal("model distribution used page contents")
	}
	if len(a.Analysis.Providers) != 1 || a.Analysis.Providers[0].Name != "workbuddy" || a.Analysis.Protocols[0].Name != "responses" {
		t.Fatal("distribution dimensions incorrect")
	}
	if len(a.Analysis.Trend) != 1 || a.Analysis.Trend[0].Stats.CachedTokens != 1760 {
		t.Fatal("trend lost cache subset")
	}
	if got := s.QueryUsage(UsageQuery{Start: 9999}); len(got.Analysis.Models) != 0 || len(got.Analysis.Trend) != 0 {
		t.Fatal("empty range retained charts")
	}
}

func TestUsageTrendCalendarDaysAndBoundedLongRanges(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	stamp := func(day, hour int) int64 { return time.Date(2026, 3, day, hour, 0, 0, 0, location).Unix() }
	a := analyzeUsage([]LogEntry{{Time: stamp(8, 23), Usage: TokenUsage{Known: true, InputTokens: 10}}, {Time: stamp(9, 0), Usage: TokenUsage{Known: true, InputTokens: 20}}}, location)
	if len(a.Trend) != 2 || a.Trend[0].Time != stamp(8, 0) || a.Trend[1].Time != stamp(9, 0) || a.Trend[1].Time-a.Trend[0].Time != 23*3600 {
		t.Fatal("calendar trend ignored daylight saving time")
	}
	long := []LogEntry{{Time: stamp(8, 0), Usage: TokenUsage{Known: true, InputTokens: 10}}, {Time: stamp(8, 0) + 4000*86400, Usage: TokenUsage{Known: true, InputTokens: 20}}}
	a = analyzeUsage(long, location)
	var total int64
	for _, p := range a.Trend {
		total += p.Stats.InputTokens
	}
	if len(a.Trend) > 90 || total != 30 {
		t.Fatal("long range unbounded or lost usage")
	}
	a = analyzeUsage([]LogEntry{{Time: stamp(8, 0), Status: 500}}, location)
	if a.Models[0].Stats.UsageRequests != 0 || a.Trend[0].Stats.UsageRequests != 0 {
		t.Fatal("unknown usage became measured zero")
	}
}
