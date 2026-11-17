package storage

import (
	"bytes"
	"codearts2api/internal/accounts"
	"encoding/json"
)

// Unlike token rotation, a reversible configuration edit must roll back when
// persistence fails. CAS also prevents overwriting a concurrent import/refresh.
func (s *Store) CommitAccountConfiguration(id string, expected, updated json.RawMessage) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID != id {
			continue
		}
		if !bytes.Equal(a.Credentials, expected) {
			return accounts.ErrCredentialChanged
		}
		before := a.Credentials
		a.Credentials = append(json.RawMessage(nil), updated...)
		raw, err := json.MarshalIndent(s.st, "", "  ")
		if err == nil {
			err = s.writeState(raw)
		}
		if err != nil {
			a.Credentials = before
		}
		return err
	}
	return accounts.ErrCredentialChanged
}
