package storage

import "codearts2api/internal/usage"

const MaxUsageRecords = usage.MaxUsageRecords

type UsageQuery = usage.UsageQuery
type UsagePage = usage.UsagePage

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

// UsageSnapshot owns its slice; no state pointer escapes the lock.
func (s *Store) UsageSnapshot() []LogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initUsageHistoryLocked()
	return append([]LogEntry(nil), s.st.UsageRecords...)
}
func (s *Store) QueryUsage(q UsageQuery) UsagePage { return usage.Query(s, q) }
func sumUsageRecord(s *Stats, e LogEntry)          { usage.SumRecord(s, e) }

// Filter before every aggregate, retention count and pagination operation.
// Neither current key names nor masked-key equality establishes ownership.
func (s *Store) QueryUserUsage(id string, q UsageQuery) UsagePage {
	s.mu.Lock()
	s.initUsageHistoryLocked()
	records := []LogEntry{}
	if id != "" {
		for _, entry := range s.st.UsageRecords {
			if entry.UserID != id {
				continue
			}
			model := entry.Model
			if entry.PublicModel != "" {
				model = entry.PublicModel
			} else if entry.RequestedModel != "" {
				model = entry.RequestedModel
			}
			// Explicit public fields: a future administrator-only log field must
			// not automatically become visible through the consumer endpoint.
			entry = LogEntry{Model: model, RequestID: entry.RequestID, RequestKind: entry.RequestKind,
				Protocol: entry.Protocol, Time: entry.Time, Stream: entry.Stream, Status: entry.Status,
				LatencyMS: entry.LatencyMS, FirstTokenMS: entry.FirstTokenMS, Usage: entry.Usage,
				UsageIncomplete: entry.UsageIncomplete, Attempts: entry.Attempts, SummaryAttempts: entry.SummaryAttempts,
				FinishReason: entry.FinishReason, EstimatedCost: entry.EstimatedCost, Err: entry.Err,
				PromptLen: entry.PromptLen, OutputLen: entry.OutputLen}
			if entry.Err != "" {
				entry.Err = "请求失败"
			}
			if c := entry.EstimatedCost; c != nil {
				entry.EstimatedCost = &usage.RequestCost{Currency: c.Currency, Amount: c.Amount, Partial: c.Partial}
			}
			records = append(records, entry)
		}
	}
	s.mu.Unlock()
	return usage.QueryRecords(records, q)
}
