package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"codearts2api/internal/store"
)

func TestBootstrapAccess(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "data", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	path, err := bootstrapAccess(st, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var access map[string]string
	if err := json.Unmarshal(raw, &access); err != nil {
		t.Fatal(err)
	}
	if len(access["admin_password"]) < 32 || access["admin_password"] == access["api_key"] {
		t.Fatal("credentials must be strong and distinct")
	}
	if !st.ValidKey(access["api_key"]) || st.ValidKey(access["admin_password"]) {
		t.Fatal("API and admin permissions must be separated")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("bootstrap secrets must be private")
	}
	loaded, err := store.Open(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !store.VerifyAdminPassword(loaded.Settings().AdminPassword, access["admin_password"]) || !loaded.ValidKey(access["api_key"]) {
		t.Fatal("bootstrap not persisted")
	}
	if loaded.Settings().WriteBackToIDE || loaded.Settings().AutoClaim {
		t.Fatal("background mutations require explicit settings")
	}
}
