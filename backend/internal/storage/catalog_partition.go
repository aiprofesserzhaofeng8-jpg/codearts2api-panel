package storage

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"time"
)

// One optional record per platform and region. Older readers can discard it.
type catalogPartition struct {
	Provider string         `json:"provider"`
	Scope    string         `json:"scope"`
	Updated  time.Time      `json:"updated_at"`
	Models   []catalogModel `json:"models"`
}
type catalogModel struct {
	provider.Model
	Kind      string                    `json:"catalog_kind,omitempty"`
	Reference *provider.ClientReference `json:"client_reference,omitempty"`
}

func catalogScope(a *accounts.Account) string {
	if a.ProviderID() == provider.CodeArts {
		return ""
	}
	return a.Scope
}
func catalogKey(a *accounts.Account) string { return a.ProviderID() + "/" + catalogScope(a) }

func (s *Store) SaveCatalogPartition(account *accounts.Account, models []provider.Model, at time.Time) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.Incarnation != account.Incarnation || a.ID != account.ID || a.Enabled != account.Enabled || a.ExternalID != account.ExternalID || catalogKey(a) != catalogKey(account) {
			continue
		}
		key := catalogKey(a)
		v := &catalogPartition{Provider: a.ProviderID(), Scope: catalogScope(a), Updated: at, Models: make([]catalogModel, len(models))}
		for i, m := range models {
			v.Models[i] = catalogModel{m, m.CatalogKind, m.ClientReference}
		}
		if s.st.catalogPartitions == nil {
			s.st.catalogPartitions = make(map[string]*catalogPartition)
		}
		old := s.st.catalogPartitions[key]
		s.st.catalogPartitions[key] = v
		if err := s.saveGroupStateLocked(); err != nil {
			if old == nil {
				delete(s.st.catalogPartitions, key)
			} else {
				s.st.catalogPartitions[key] = old
			}
			return err
		}
		return nil
	}
	return accounts.ErrCredentialChanged
}

func (s *Store) RestoreCatalogPartitions(load func(string, string, []provider.Model, time.Time)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	for _, a := range s.st.Accounts {
		key := catalogKey(a)
		v := s.st.catalogPartitions[key]
		if !a.Enabled || seen[key] || v == nil {
			continue
		}
		seen[key] = true
		models := make([]provider.Model, len(v.Models))
		for i, m := range v.Models {
			models[i] = m.Model
			models[i].CatalogKind = m.Kind
			models[i].ClientReference = m.Reference
		}
		load(v.Provider, v.Scope, models, v.Updated)
	}
}

func (s *Store) CatalogPartitionStatus(platform, scope string) (int, *time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.st.catalogPartitions[platform+"/"+scope]
	if v == nil {
		return 0, nil
	}
	at := v.Updated
	return len(v.Models), &at
}
