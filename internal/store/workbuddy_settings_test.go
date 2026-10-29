package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkBuddySettingsDefaultsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":4,"settings":{"auto_claim":false}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Settings().WorkBuddy != DefaultWorkBuddySettings() {
		t.Fatal("legacy state lost defaults")
	}
	want := WorkBuddySettings{PromptMode: "passthrough", PromptText: "custom text", SearchScope: "global"}
	s.UpdateSettings(func(st *Settings) { st.WorkBuddy = want })
	if err = s.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Settings().WorkBuddy != want {
		t.Fatal("disabled switches lost on reload")
	}
}
