package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
)

// accountJSON is the schema-6 compatibility boundary for the legacy session.
// Shared accounts hold only an opaque immutable credential view. Decode the
// concrete CodeArts value once on load, preserving pointer generation checks.
type accountJSON struct {
	accounts.Account
	Session       *codearts.Session       `json:"session,omitempty"`
	ModelSnapshot *accounts.ModelSnapshot `json:"model_snapshot,omitempty"`
}

// Keep optional observations inside account records for schema-11 readers.
func (s State) MarshalJSON() ([]byte, error) {
	type plain State
	if len(s.accountModelSnapshots) == 0 {
		return json.Marshal((*plain)(&s))
	}
	type record struct {
		*accounts.Account
		Snapshot *accounts.ModelSnapshot `json:"model_snapshot,omitempty"`
	}
	records := make([]*record, len(s.Accounts))
	for i, a := range s.Accounts {
		if a != nil {
			records[i] = &record{a, s.accountModelSnapshots[a.ID]}
		}
	}
	return json.Marshal(struct {
		*plain
		Accounts []*record `json:"accounts"`
	}{(*plain)(&s), records})
}
func (s *State) UnmarshalJSON(raw []byte) error {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.Version == 10 || header.Version == 11 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		known := map[string]bool{}
		typ := reflect.TypeOf(State{})
		for i := 0; i < typ.NumField(); i++ {
			name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				known[name] = true
			}
		}
		for name := range fields {
			if !known[name] {
				return fmt.Errorf("状态包含未知顶层字段，拒绝丢弃数据")
			}
		}
		if groupData, ok := fields["groups"]; ok {
			var groups []json.RawMessage
			if err := json.Unmarshal(groupData, &groups); err != nil {
				return err
			}
			for _, rawGroup := range groups {
				var group AccountGroup
				d := json.NewDecoder(bytes.NewReader(rawGroup))
				d.DisallowUnknownFields()
				if err := d.Decode(&group); err != nil {
					return fmt.Errorf("分组状态字段无效，拒绝丢弃权限")
				}
			}
		}
	}
	if header.Version == 11 {
		if err := decodeNewRouting(raw); err != nil {
			return err
		}
	}
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
		if v := record.ModelSnapshot; v != nil && v.List.AccountID == record.ID && v.List.Provider == record.ProviderID() && v.List.Scope == record.Scope {
			if s.accountModelSnapshots == nil {
				s.accountModelSnapshots = make(map[string]*accounts.ModelSnapshot)
			}
			s.accountModelSnapshots[record.ID] = v
		}
		s.Accounts[i] = &record.Account
	}
	return nil
}
