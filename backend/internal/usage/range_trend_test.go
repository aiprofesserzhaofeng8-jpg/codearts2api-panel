package usage

import (
	"testing"
	"time"
)

func TestRangeTrendAggregatesReportedHours(t *testing.T) {
	known := TokenUsage{Known: true, InputTokens: 10}
	rows := []LogEntry{{Time: 18100, Usage: known}, {Time: 10801}, {Time: 7300, Usage: known}, {Time: 7201, Usage: known}}
	h := analyzeTrend(rows, time.UTC, "hour")
	if len(h.Trend) != 2 || h.Trend[0].Time != 7200 || h.Trend[0].Stats.InputTokens != 20 || h.Trend[1].Time != 18000 {
		t.Fatalf("hourly points: %+v", h.Trend)
	}
	d := analyzeTrend(rows, time.UTC, "day")
	if len(d.Trend) != 1 || d.Trend[0].Stats.InputTokens != 30 || d.Trend[0].Stats.TotalRequests != 4 {
		t.Fatalf("daily points: %+v", d.Trend)
	}
}
