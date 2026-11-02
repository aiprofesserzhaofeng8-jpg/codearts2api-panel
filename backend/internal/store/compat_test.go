package store_test

import (
	"path/filepath"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/storage"
	"codearts2api/internal/store"
	"codearts2api/internal/usage"
)

func TestFacadeRetainsTypeAndTransactionIdentity(t *testing.T) {
	legacy, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var current *storage.Store = legacy
	key := legacy.AddKey("fixture")
	if err := current.SetKeyBudgetAndSave(key.Key, access.KeyBudget{MaxRequests: 1}, false); err != nil {
		t.Fatal(err)
	}
	release, err := legacy.BeginKeyBudget(key.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := current.BeginKeyBudget(key.Key); err == nil {
		t.Fatal("facade created a second reservation domain")
	}
	release()
	var entry store.LogEntry = usage.LogEntry{Status: 200, Model: "fixture-v1"}
	current.Record(entry, 0, key.Key)
	if current.Stats().TotalRequests != 1 || store.ErrUsagePersistence != storage.ErrUsagePersistence {
		t.Fatal("facade type or error identity changed")
	}
}
