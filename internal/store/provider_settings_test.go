package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderSettingsMigrationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":4,"settings":{"workbuddy":{"prompt_mode":"passthrough","sanitize":false}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codearts", "trae"} {
		if s.Settings().ProviderPolicy(name) != DefaultProviderSettings(name) {
			t.Fatal("missing migration defaults")
		}
	}
	if s.Settings().WorkBuddy.Sanitize || s.Settings().WorkBuddy.PromptMode != "passthrough" {
		t.Fatal("existing WorkBuddy config lost")
	}
	ca, tr := DefaultProviderSettings("codearts"), DefaultProviderSettings("trae")
	ca.PromptMode = "custom"
	ca.PromptText = "CA"
	ca.PromptFallback = false
	ca.Sanitize = false
	ca.SearchEnabled = true
	ca.SearchURL = "http://127.0.0.1:1234/search"
	tr.PromptText = "TR"
	tr.PromptFallback = false
	tr.Sanitize = false
	s.UpdateSettings(func(st *Settings) { st.CodeArts = ca; st.Trae = tr })
	if err = s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Settings().CodeArts != ca || loaded.Settings().Trae != tr || loaded.Settings().WorkBuddy.Sanitize {
		t.Fatal("independent settings/false values lost")
	}
}
