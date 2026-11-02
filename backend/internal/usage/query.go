package usage

import (
	"sort"
	"time"
)

const MaxUsageRecords = 100000

type UsageQuery struct {
	Provider       string
	Model          string
	Protocol       string
	Status         string
	Location       *time.Location
	Start, End     int64
	Page, PageSize int
}
type UsagePage struct {
	Analysis UsageAnalysis `json:"analysis"`
	Logs     []LogEntry    `json:"logs"`
	Stats    Stats         `json:"stats"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Pages    int           `json:"pages"`
	Retained int           `json:"retained"`
	Oldest   int64         `json:"oldest"`
	Limit    int           `json:"retention_limit"`
}

// History supplies a caller-owned snapshot published under its transaction lock.
type History interface{ UsageSnapshot() []LogEntry }

func Query(source History, q UsageQuery) UsagePage { return query(source.UsageSnapshot(), q) }

// QueryRecords takes a caller-owned, permission-filtered snapshot.
func QueryRecords(records []LogEntry, q UsageQuery) UsagePage { return query(records, q) }

func query(records []LogEntry, q UsageQuery) UsagePage {
	result := UsagePage{Logs: []LogEntry{}, Page: q.Page, PageSize: q.PageSize, Retained: len(records), Limit: MaxUsageRecords}
	if result.PageSize < 1 || result.PageSize > 100 {
		result.PageSize = 20
	}
	if result.Page < 1 {
		result.Page = 1
	}
	matched := make([]LogEntry, 0, len(records))
	// Reverse before stable sorting so equal-second completions stay newest-first.
	for i := len(records) - 1; i >= 0; i-- {
		e := records[i]
		if result.Oldest == 0 || e.Time < result.Oldest {
			result.Oldest = e.Time
		}
		if q.Start != 0 && e.Time < q.Start || q.End != 0 && e.Time >= q.End {
			continue
		}
		name := e.Provider
		if name == "" {
			name = "codearts"
		}
		if q.Provider != "" && name != q.Provider {
			continue
		}
		if q.Model != "" && e.Model != q.Model || q.Protocol != "" && e.Protocol != q.Protocol {
			continue
		}
		failed := e.Status >= 400 || e.Err != ""
		if q.Status == "success" && failed || q.Status == "error" && !failed {
			continue
		}
		matched = append(matched, e)
		SumRecord(&result.Stats, e)
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Time > matched[j].Time })
	result.Analysis = Analyze(matched, q.Location)
	result.Total = len(matched)
	result.Pages = max(1, (result.Total+result.PageSize-1)/result.PageSize)
	result.Page = min(result.Page, result.Pages)
	start := (result.Page - 1) * result.PageSize
	result.Logs = append(result.Logs, matched[start:min(start+result.PageSize, len(matched))]...)
	return result
}
func SumRecord(s *Stats, e LogEntry) {
	if e.RequestKind == "summary" {
		s.SummaryRequests++
		s.SummaryRetries += int64(max(0, e.SummaryAttempts-1))
		if e.Status >= 400 {
			s.SummaryFailures++
		}
	}
	if e.FinishReason == "length" {
		s.TruncatedRequests++
	}
	if e.UsageIncomplete {
		s.PartialUsageRequests++
	}
	s.TotalRequests++
	s.TotalLatencyMS += max(0, e.LatencyMS)
	s.LatencySamples++
	if e.Status >= 400 || e.Err != "" {
		s.FailedRequests++
	}
	if e.Usage.Known {
		s.UsageRequests++
		s.InputTokens += e.Usage.InputTokens
		s.OutputTokens += e.Usage.OutputTokens
		s.CachedTokens += e.Usage.CachedTokens
		s.ReasoningTokens += e.Usage.ReasoningTokens
	}
	if c := e.EstimatedCost; c != nil && c.Currency == "USD" {
		s.PricedRequests++
		s.PricedUSDRequests++
		s.EstimatedUSD += c.Amount
	}
}
