package store

import "codearts2api/internal/codearts"

// Session values are immutable. Pointer comparison checks the exact generation
// read under the refresh lock, including a re-login while the network was busy.
func (s *Store) CommitAccountSession(id string, expected, updated *codearts.Session) error {
	s.mu.Lock()
	found := false
	for _, a := range s.st.Accounts {
		if a.ID != id {
			continue
		}
		if a.Session != expected {
			s.mu.Unlock()
			return codearts.ErrSessionChanged
		}
		a.Session = updated
		found = true
		break
	}
	s.mu.Unlock()
	if !found {
		return codearts.ErrSessionChanged
	}
	// A rotated refresh token must remain in memory even if disk temporarily fails.
	return s.Save()
}
