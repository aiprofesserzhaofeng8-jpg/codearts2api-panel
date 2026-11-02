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
