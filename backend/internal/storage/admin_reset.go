package storage

import "time"

// Administrative resets use the same serialized transaction as other local
// settings. Usage history and the settlement journal are never reset here.
func (s *Store) ClearLogsAndSave() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	logs, history, initialized := s.st.Logs, s.st.UsageRecords, s.st.UsageHistoryInitialized
	s.initUsageHistoryLocked()
	s.st.Logs = nil
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.Logs, s.st.UsageRecords, s.st.UsageHistoryInitialized = logs, history, initialized
		return err
	}
	return nil
}

func (s *Store) ClearStatsAndSave() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.st.Stats
	s.st.Stats = Stats{StartedAt: time.Now().Unix()}
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.Stats = before
		return err
	}
	return nil
}
