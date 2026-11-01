package store

// Model failures are independent of account credentials. A cooling model must
// not prevent the same account serving another model.
type ModelHealth struct {
	Failures      int    `json:"failures"`
	CooldownUntil int64  `json:"cooldown_until"`
	UpdatedAt     int64  `json:"updated_at"`
	LastError     string `json:"last_error,omitempty"`
}

func (s *Store) MarkModelResult(id, model string, ok bool, reason string, cooldown int) {
	if model == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, a := range s.st.Accounts {
		if a.ID == id {
			found = true
			break
		}
	}
	if !found {
		return
	}
	if s.st.ModelHealth == nil {
		s.st.ModelHealth = map[string]map[string]ModelHealth{}
	}
	if s.st.ModelHealth[id] == nil {
		s.st.ModelHealth[id] = map[string]ModelHealth{}
	}
	models := s.st.ModelHealth[id]
	if _, exists := models[model]; !exists && len(models) >= 256 {
		return
	}
	v := models[model]
	v.UpdatedAt = s.poolNow().Unix()
	if ok {
		v.Failures = 0
		v.CooldownUntil = 0
		v.LastError = ""
	} else {
		v.Failures++
		v.LastError = reason
		if cooldown > 0 {
			v.CooldownUntil = max(v.CooldownUntil, v.UpdatedAt+int64(cooldown))
		}
	}
	models[model] = v
}
func (s *Store) ModelHealthSnapshot() map[string]map[string]ModelHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]map[string]ModelHealth{}
	for _, a := range s.st.Accounts {
		out[a.ID] = map[string]ModelHealth{}
		for k, v := range s.st.ModelHealth[a.ID] {
			out[a.ID][k] = v
		}
	}
	return out
}
func (s *Store) ClearModelHealth(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.st.ModelHealth, id)
}
