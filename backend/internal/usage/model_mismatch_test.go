package usage

import "testing"

func TestModelMismatchBeforePagination(t *testing.T) {
	records := []LogEntry{
		{Time: 6, Model: "old"}, {Time: 5, RequestedModel: "unknown"},
		{Time: 4, RequestedModel: "same", Model: "same"},
		{Time: 3, RequestedModel: "public", Model: "a"},
		{Time: 2, RequestedModel: "public", Model: "b"},
	}
	p := QueryRecords(records, UsageQuery{ModelMismatch: true, Page: 2, PageSize: 1})
	if p.Total != 2 || p.Pages != 2 || p.Stats.TotalRequests != 2 || len(p.Logs) != 1 || p.Logs[0].Model != "b" {
		t.Fatalf("unexpected filtered page: %+v", p)
	}
}
