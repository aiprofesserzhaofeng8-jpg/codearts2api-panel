package storage

import (
	"encoding/json"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
)

// accountJSON is the schema-6 compatibility boundary for the legacy session.
// Shared accounts hold only an opaque immutable credential view. Decode the
// concrete CodeArts value once on load, preserving pointer generation checks.
type accountJSON struct {
	accounts.Account
	Session *codearts.Session `json:"session,omitempty"`
}

func (s *State) UnmarshalJSON(raw []byte) error {
	type stateJSON State
	wire := struct {
		*stateJSON
		Accounts json.RawMessage `json:"accounts"`
	}{stateJSON: (*stateJSON)(s)}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	if wire.Accounts == nil {
		return nil
	}
	var records []*accountJSON
	if err := json.Unmarshal(wire.Accounts, &records); err != nil {
		return err
	}
	if records == nil {
		s.Accounts = nil
		return nil
	}
	s.Accounts = make([]*accounts.Account, len(records))
	for i, record := range records {
		if record == nil {
			continue
		}
		if record.Session != nil {
			record.Account.NativeCredential = record.Session
		} else {
			// Do not put a typed nil pointer into the interface.
			record.Account.NativeCredential = nil
		}
		s.Accounts[i] = &record.Account
	}
	return nil
}
