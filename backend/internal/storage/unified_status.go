package storage

import (
	"time"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// Snapshot only: same account/model/concurrency conditions as pool admission,
// without selecting, reserving, refreshing credentials or marking probe success.
func (s *Store) UnifiedSourceCounts(ids []string) (map[string]int, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.poolNow()
	out := make(map[string]int, len(ids))
	for _, id := range ids {
		raw := config.RawSource(id)
		platform, region, _ := provider.ParseModel(raw)
		for _, a := range s.st.Accounts {
			if a.ProviderID() == platform && (platform == provider.CodeArts || a.Scope == region) && a.Enabled && !a.Paused && a.CooldownUntil <= now.Unix() && s.st.ModelHealth[a.ID][raw].CooldownUntil <= now.Unix() && s.pool.inFlight[a.ID] < accountMaxInFlight(a, s.st.Settings.Pool) {
				out[id]++
			}
		}
	}
	return out, now.UTC()
}
