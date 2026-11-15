package storage

// ChangeKeysAndSave uses the existing persistence and state locks. Deletion
// precedes toggling; missing references retain their historical no-op behavior.
func (s *Store) ChangeKeysAndSave(deleteKey, toggleKey string) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.st.Keys
	next := make([]*APIKey, 0, len(before))
	deleted := false
	toggled := false
	for _, key := range before {
		if deleteKey != "" && key.Key == deleteKey && !deleted {
			deleted = true
			continue
		}
		copy := *key
		if toggleKey != "" && key.Key == toggleKey && !toggled {
			copy.Enabled = !copy.Enabled
			toggled = true
		}
		next = append(next, &copy)
	}
	s.st.Keys = next
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.Keys = before
		return err
	}
	return nil
}
