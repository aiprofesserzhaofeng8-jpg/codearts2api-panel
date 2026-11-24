package storage

import (
	"path/filepath"
	"testing"
)

func TestMobileClawSearchSettingsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if p := s.Settings().MobileClaw; p.ValidateFor("mobileclaw") != nil || p.SearchEnabled || p.SearchBackend != "native" {
		t.Fatalf("invalid default %+v", p)
	}
	if err = s.UpdateSettingsAndSave(func(st *Settings) { st.MobileClaw.SearchEnabled = true }); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if p := reopened.Settings().ProviderPolicy("mobileclaw"); !p.SearchEnabled || p.SearchBackend != "native" {
		t.Fatal("search settings lost")
	}
}
