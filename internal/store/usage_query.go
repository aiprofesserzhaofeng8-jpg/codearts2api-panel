package store

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

// Bootstrap from surviving logs once. Usage history then has its own retention
// and survives operational log cleanup. Already deleted records cannot be rebuilt.
func (s *Store) initUsageHistoryLocked() {
	if s.st.UsageHistoryInitialized {
		return
	}
	logs := s.st.Logs
	if len(logs) > MaxUsageRecords {
		logs = logs[len(logs)-MaxUsageRecords:]
	}
	s.st.UsageRecords = append([]LogEntry(nil), logs...)
	s.st.UsageHistoryInitialized = true
}
func (s *Store) QueryUsage(q UsageQuery) UsagePage {
	s.mu.Lock()
	s.initUsageHistoryLocked()
	records := append([]LogEntry(nil), s.st.UsageRecords...)
	s.mu.Unlock()
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
		sumUsageRecord(&result.Stats, e)
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Time > matched[j].Time })
	result.Analysis = analyzeUsage(matched, q.Location)
	result.Total = len(matched)
	result.Pages = max(1, (result.Total+result.PageSize-1)/result.PageSize)
	result.Page = min(result.Page, result.Pages)
	start := (result.Page - 1) * result.PageSize
	result.Logs = append(result.Logs, matched[start:min(start+result.PageSize, len(matched))]...)
	return result
}
func sumUsageRecord(s *Stats, e LogEntry) {
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
