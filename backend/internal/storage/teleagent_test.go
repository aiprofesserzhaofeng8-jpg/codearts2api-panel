package storage

import (
	"path/filepath"
	"testing"

	"codearts2api/internal/config"
)

func TestTeleagentAdditiveSettingsPersistenceAndSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if p := s.Settings().TeleAgent; p.ValidateFor("teleagent") != nil || p.PromptOverrides == nil {
		t.Fatal("new provider default invalid")
	}
	value := "TeleAgent custom"
	s.UpdateSettings(func(st *Settings) { st.TeleAgent.PromptOverrides = &config.PromptOverrides{Text: &value} })
	snapshot := s.Settings()
	*snapshot.TeleAgent.PromptOverrides.Text = "mutated snapshot"
	if *s.Settings().TeleAgent.PromptOverrides.Text != "TeleAgent custom" {
		t.Fatal("prompt snapshot aliased")
	}
	if e = s.Save(); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(path)
	if e != nil || *reopened.Settings().TeleAgent.PromptOverrides.Text != "TeleAgent custom" {
		t.Fatal("provider settings lost", e)
	}
	if policy := reopened.Settings().ProviderPolicy("teleagent"); policy.PromptText != "TeleAgent custom" || policy.SearchEnabled {
		t.Fatal("provider prompt inheritance lost")
	}
}
