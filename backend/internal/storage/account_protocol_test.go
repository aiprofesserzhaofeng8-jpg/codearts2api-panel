package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProtocolConfigurationCASAndRollback(t *testing.T) {
	s := testPool(t, 1)
	initial := s.Account("0").Credentials
	next := json.RawMessage(`{"api_key":"fixture","api_protocol":"adaptive"}`)
	if err := s.CommitAccountConfiguration("0", initial, next); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Path())
	var compact bytes.Buffer
	if err == nil {
		err = json.Compact(&compact, reopened.Account("0").Credentials)
	}
	if err != nil || compact.String() != string(next) {
		t.Fatal(err)
	}
	if err := s.CommitAccountConfiguration("0", initial, []byte(`{}`)); err == nil {
		t.Fatal("stale write accepted")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, []byte("x"), 0600)
	s.path = filepath.Join(blocker, "state.json")
	if err := s.CommitAccountConfiguration("0", next, []byte(`{}`)); err == nil {
		t.Fatal("failed persistence accepted")
	}
	if string(s.Account("0").Credentials) != string(next) {
		t.Fatal("failed save changed live settings")
	}
}
