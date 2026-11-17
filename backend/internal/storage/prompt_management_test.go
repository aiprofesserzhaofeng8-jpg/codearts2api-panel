package storage

import (
	"codearts2api/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPromptEditorIsolationPersistenceAndConflict(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := st.Settings().PromptConfiguration()
	candidate := st.Settings().PromptConfiguration()
	candidate.Common.Mode = "append"
	candidate.Common.Text = "EXTRA"
	if err := st.UpdateSettingsAndSave(func(s *Settings) { s.RequestTimeout = 123 }); err != nil {
		t.Fatal(err)
	}
	if err := st.SavePromptConfiguration(before, candidate); err != nil {
		t.Fatal(err)
	}
	if st.Settings().RequestTimeout != 123 {
		t.Fatal("prompt save clobbered runtime settings")
	}
	reopened, err := Open(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.Settings().PromptConfiguration(), candidate) {
		t.Fatal("config not persisted")
	}
	if err := st.SavePromptConfiguration(before, candidate); err == nil {
		t.Fatal("stale editor silently overwrote config")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, []byte("x"), 0600)
	st.path = filepath.Join(blocker, "state.json")
	next := st.Settings().PromptConfiguration()
	next.Common.Text = "FAILED"
	if err := st.SavePromptConfiguration(candidate, next); err == nil {
		t.Fatal("write failure accepted")
	}
	if !reflect.DeepEqual(st.Settings().PromptConfiguration(), candidate) {
		t.Fatal("failed save changed effective instructions")
	}
}
func TestPromptProjectionPreservesLegacyOverrides(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateSettings(func(s *Settings) {
		s.CommonPrompt = config.PromptSettings{Mode: "custom", Text: "COMMON"}
		s.WorkBuddy.PromptMode = "custom"
		s.WorkBuddy.PromptText = "LEGACY"
		s.WorkBuddy.PromptOverrides = nil
	})
	before := st.Settings().ProviderPolicy("workbuddy")
	configuration := st.Settings().PromptConfiguration()
	if err := st.SavePromptConfiguration(configuration, configuration); err != nil {
		t.Fatal(err)
	}
	after := st.Settings().ProviderPolicy("workbuddy")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("moving editor changed legacy behavior")
	}
}
