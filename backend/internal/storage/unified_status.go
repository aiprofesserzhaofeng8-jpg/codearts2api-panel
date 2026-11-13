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

// SourceRuntimeStatus reads the original state under its existing lock. It
// never probes, refreshes, reserves capacity or changes account scheduling.
func (s *Store) SourceRuntimeStatus(ids []string) map[string]config.SourceRuntimeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.poolNow().Unix()
	out := make(map[string]config.SourceRuntimeStatus, len(ids))
	rawKeys := map[string]string{}
	for _, id := range ids {
		raw := config.RawSource(id)
		rawKeys[raw] = id
		platform, region, _ := provider.ParseModel(raw)
		v := config.SourceRuntimeStatus{}
		for _, a := range s.st.Accounts {
			if a.ProviderID() != platform || platform != provider.CodeArts && a.Scope != region {
				continue
			}
			v.AccountCount++
			until := max(a.CooldownUntil, s.st.ModelHealth[a.ID][raw].CooldownUntil)
			switch {
			case !a.Enabled:
				v.Disabled++
			case a.Paused:
				v.Paused++
			case until > now:
				v.Cooling++
				if v.NextRetryAt == 0 || until < v.NextRetryAt {
					v.NextRetryAt = until
				}
			case s.pool.inFlight[a.ID] >= accountMaxInFlight(a, s.st.Settings.Pool):
				v.Busy++
			default:
				v.Ready++
			}
		}
		out[id] = v
	}
	// Scan the existing bounded request log once, never copy prompts or keys.
	for i := len(s.st.Logs) - 1; i >= 0 && len(rawKeys) > 0; i-- {
		entry := s.st.Logs[i]
		key, ok := rawKeys[entry.Model]
		if !ok {
			continue
		}
		v := out[key]
		v.LastRequest = &config.SourceRequestStatus{CheckedAt: entry.Time, Status: entry.Status, ErrorKind: entry.ErrorKind}
		out[key] = v
		delete(rawKeys, entry.Model)
	}
	return out
}
