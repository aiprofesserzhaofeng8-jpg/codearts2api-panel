package storage

import (
	"encoding/json"
	"errors"
	"fmt"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
)

// ErrAccountExists preserves the existing store API and error identity.
var ErrAccountExists = accounts.ErrAccountExists

// Persist the override atomically; a failed write must not change live admission.
func (s *Store) SetAccountConcurrencyAndSave(id string, limit int) (bool, error) {
	if limit < 0 || limit > 32 {
		return false, fmt.Errorf("账号并发上限必须在 0–32 之间，0 表示使用全局默认值")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID != id {
			continue
		}
		before := a.MaxInFlight
		a.MaxInFlight = limit
		raw, err := json.MarshalIndent(s.st, "", "  ")
		if err == nil {
			err = s.writeState(raw)
		}
		if err != nil {
			a.MaxInFlight = before
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func sameAccountSession(a, b *codearts.Session) bool {
	return a != nil && b != nil && ((a.DomainID != "" && a.DomainID == b.DomainID) || (a.AccessKey != "" && a.AccessKey == b.AccessKey))
}

func (s *Store) FindAccountSession(snapshot any) *accounts.Account {
	// The legacy session codec is still interpreted only by its owning platform.
	sess, _ := snapshot.(*codearts.Session)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.st.Accounts {
		if sameAccountSession(codearts.StoredSession(a), sess) {
			return cloneAccount(a)
		}
	}
	return nil
}

func (s *Store) UpsertAccountAndSave(a *accounts.Account, replace bool) (bool, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if a == nil {
		return false, errors.New("缺少账号")
	}
	if !replace {
		for _, ex := range s.st.Accounts {
			if ex.ProviderID() == a.ProviderID() && ex.Scope == a.Scope && (sameAccountSession(codearts.StoredSession(ex), codearts.StoredSession(a)) || (a.ExternalID != "" && a.ExternalID == ex.ExternalID)) {
				return false, ErrAccountExists
			}
		}
	}
	before := cloneAccounts(s.st.Accounts)
	isNew := s.upsertAccountLocked(a)
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err == nil {
		err = s.writeState(raw)
	}
	if err != nil {
		s.st.Accounts = before
		return false, err
	}
	return isNew, nil
}

// Delete only succeeds if the new state has reached disk. On failure, restore the
// in-memory list so the UI cannot report a deletion which returns after restart.
func (s *Store) RemoveAccountAndSave(id string) (bool, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, a := range s.st.Accounts {
		if a.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil
	}
	before := cloneAccounts(s.st.Accounts)
	s.st.Accounts = append(s.st.Accounts[:idx], s.st.Accounts[idx+1:]...)
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err == nil {
		err = s.writeState(raw)
	}
	if err != nil {
		s.st.Accounts = before
		return false, err
	}
	for key, b := range s.pool.bindings {
		if b.id == id {
			delete(s.pool.bindings, key)
		}
	}
	return true, nil
}

func (s *Store) FindProviderAccount(name, scope, id string) *accounts.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.st.Accounts {
		if a.ProviderID() == name && a.Scope == scope && a.ExternalID == id {
			return cloneAccount(a)
		}
	}
	return nil
}

// ImportAccountsAndSave commits the complete import under the existing store locks.
func (s *Store) ImportAccountsAndSave(list []*accounts.Account) ([]bool, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	before := cloneAccounts(s.st.Accounts)
	created := make([]bool, len(list))
	for i, a := range list {
		if a == nil {
			s.st.Accounts = before
			return nil, errors.New("缺少账号")
		}
		created[i] = s.upsertAccountLocked(a)
	}
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err == nil {
		err = s.writeState(raw)
	}
	if err != nil {
		s.st.Accounts = before
		return nil, err
	}
	return created, nil
}

// ChangeAccountAndSave applies admin controls atomically, including model health.
// No credential refresh is rolled back by this transaction.
func (s *Store) ChangeAccountAndSave(id, action string) (bool, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID != id {
			continue
		}
		before := *a
		health, hadHealth := s.st.ModelHealth[id]
		switch action {
		case "toggle":
			a.Enabled = !a.Enabled
		case "pause":
			a.Paused = true
		case "resume":
			a.Paused = false
		case "clear-errors":
			a.Failures = 0
			a.Status = "ok"
			a.LastErr = ""
			a.CooldownUntil = 0
			a.CooldownReason = ""
			delete(s.st.ModelHealth, id)
		default:
			return false, errors.New("未知账号操作")
		}
		raw, err := json.MarshalIndent(s.st, "", "  ")
		if err == nil {
			err = s.writeState(raw)
		}
		if err != nil {
			*a = before
			if hadHealth {
				s.st.ModelHealth[id] = health
			}
			return false, err
		}
		return true, nil
	}
	return false, nil
}
