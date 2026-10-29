package store

import (
	"encoding/json"
	"errors"
	"fmt"

	"codearts2api/internal/codearts"
)

var ErrAccountExists = errors.New("该账号已存在，请确认更新已有账号")

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

func (s *Store) FindAccountSession(sess *codearts.Session) *codearts.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.st.Accounts {
		if sameAccountSession(a.Session, sess) {
			return cloneAccount(a)
		}
	}
	return nil
}

func (s *Store) UpsertAccountAndSave(a *codearts.Account, replace bool) (bool, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if a == nil {
		return false, errors.New("缺少账号")
	}
	if !replace {
		for _, ex := range s.st.Accounts {
			if ex.ProviderID() == a.ProviderID() && ex.Scope == a.Scope && (sameAccountSession(ex.Session, a.Session) || (a.ExternalID != "" && a.ExternalID == ex.ExternalID)) {
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

func (s *Store) FindProviderAccount(name, scope, id string) *codearts.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.st.Accounts {
		if a.ProviderID() == name && a.Scope == scope && a.ExternalID == id {
			return cloneAccount(a)
		}
	}
	return nil
}
