package storage

import "codearts2api/internal/provider"

// Preserve empty owner sets until restart so a request resolved just before
// the last owner's deletion cannot fall through to an unrelated account.
func (s *Store) rebuildPublishedLocked() {
	next := make(map[string]provider.Model)
	for id := range s.published {
		next[id] = provider.Model{}
	}
	for _, a := range s.st.Accounts {
		snap := s.st.accountModelSnapshots[a.ID]
		if snap == nil || snap.List.Scope != a.Scope || snap.List.Provider != a.ProviderID() {
			continue
		}
		for raw, v := range snap.Published {
			id := provider.ModelID(a.ProviderID(), a.Scope, raw)
			m := next[id]
			if m.ID == "" {
				m = provider.Model{ID: id, Name: v.Name, Provider: a.ProviderID(), Scope: a.Scope, UpstreamID: raw, Source: "account_sync", Context: v.Context, MaxOutput: v.MaxOutput, StreamOnly: !v.NonStreaming, Credit: v.Credit, ImageInput: v.ImageInput, ToolCalling: v.ToolCalling, SupportedProtocols: v.Protocols, SyncedOwners: map[string]bool{}}
			}
			m.SyncedOwners[a.ID] = true
			next[id] = m
		}
	}
	s.published = next
}

// PublishedModels returns an immutable generation. Neither readers nor future
// commits mutate its map or its owner sets.
func (s *Store) PublishedModels() map[string]provider.Model {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.published
}
func (s *Store) IsPublishedModel(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.published[id]
	return ok
}
