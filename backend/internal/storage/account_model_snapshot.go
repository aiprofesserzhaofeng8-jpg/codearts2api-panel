package storage

import (
	"codearts2api/internal/accounts"
	"encoding/json"
	"maps"
	"slices"
)

// The pointer identifies the stored account generation, including delete/recreate.
type ModelSyncTarget struct{ account *accounts.Account }

func cloneModelSnapshot(v *accounts.ModelSnapshot) *accounts.ModelSnapshot {
	if v == nil {
		return nil
	}
	n := *v
	n.Published = maps.Clone(v.Published)
	n.List.Models = slices.Clone(v.List.Models)
	n.Delta.Added = slices.Clone(v.Delta.Added)
	n.Delta.Missing = slices.Clone(v.Delta.Missing)
	n.Delta.Retained = slices.Clone(v.Delta.Retained)
	return &n
}
func (s *Store) ModelSyncTarget(id string) (ModelSyncTarget, *accounts.ModelSnapshot) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.st.Accounts {
		if a.ID == id {
			return ModelSyncTarget{a}, cloneModelSnapshot(s.st.accountModelSnapshots[id])
		}
	}
	return ModelSyncTarget{}, nil
}
func (s *Store) SaveModelSnapshot(target ModelSyncTarget, v *accounts.ModelSnapshot) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	id := v.List.AccountID
	for _, a := range s.st.Accounts {
		if a != target.account || !a.Enabled || a.ID != id || a.Scope != v.List.Scope || a.ProviderID() != v.List.Provider {
			continue
		}
		if s.st.accountModelSnapshots == nil {
			s.st.accountModelSnapshots = make(map[string]*accounts.ModelSnapshot)
		}
		old := s.st.accountModelSnapshots[id]
		s.st.accountModelSnapshots[id] = cloneModelSnapshot(v)
		raw, err := json.MarshalIndent(s.st, "", "  ")
		if err == nil {
			err = s.writeState(raw)
		}
		if err != nil {
			if old == nil {
				delete(s.st.accountModelSnapshots, id)
			} else {
				s.st.accountModelSnapshots[id] = old
			}
		}
		if err == nil {
			s.rebuildPublishedLocked()
		}
		return err
	}
	return accounts.ErrCredentialChanged
}
