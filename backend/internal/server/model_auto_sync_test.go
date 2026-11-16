package server

import (
	"reflect"
	"testing"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func TestDiscoverySyncCreatesAppendsAndPreservesChoices(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "glm-5.3"}})
	if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
		t.Fatal(err)
	}
	find := func() config.UnifiedModel {
		for _, m := range s.Store.UnifiedSnapshot().Models {
			if m.Name == "glm-5.3" {
				return m
			}
		}
		t.Fatal("first discovery did not create shared name")
		return config.UnifiedModel{}
	}
	m := find()
	m.Enabled = false
	m.Sources[0].Enabled = false
	if _, err := s.UnifiedModels.Save(m, nil, m.Revision, s.Store.UnifiedSnapshot().Revision); err != nil {
		t.Fatal(err)
	}
	groups := s.Store.CanonicalGroups()
	s.Reg.ReplaceProvider(provider.MobileClaw, "cn", []provider.Model{{ID: "glm-5.3"}})
	if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
		t.Fatal(err)
	}
	got := find()
	if got.ID != m.ID || got.Enabled || len(got.Sources) != len(m.Sources)+1 {
		t.Fatalf("unexpected reconciliation: %+v", got)
	}
	for _, old := range m.Sources {
		found := false
		for _, src := range got.Sources {
			if src == old {
				found = true
			}
		}
		if !found {
			t.Fatal("existing source changed")
		}
	}
	if !reflect.DeepEqual(groups, s.Store.CanonicalGroups()) {
		t.Fatal("group permissions changed")
	}
	before := s.Store.UnifiedSnapshot()
	if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Store.UnifiedSnapshot()) {
		t.Fatal("unchanged refresh wrote new revisions")
	}
	reopened, err := storage.Open(s.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, reopened.UnifiedSnapshot()) {
		t.Fatal("sync not durable")
	}
}

func TestOfficialAPIDiscoveryPreservesChoices(t *testing.T) {
	for _, platform := range []string{provider.DeepSeek, provider.Zhipu, provider.Kimi, provider.MiniMax, provider.Gemini} {
		t.Run(platform, func(t *testing.T) {
			s, _ := unifiedAPIFixture(t)
			s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "glm-5.3"}})
			if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
				t.Fatal(err)
			}
			find := func() config.UnifiedModel {
				for _, m := range s.Store.UnifiedSnapshot().Models {
					if m.Name == "glm-5.3" {
						return m
					}
				}
				t.Fatal("first discovery did not create shared name")
				return config.UnifiedModel{}
			}
			m := find()
			m.Enabled = false
			m.Sources[0].Enabled = false
			if _, err := s.UnifiedModels.Save(m, nil, m.Revision, s.Store.UnifiedSnapshot().Revision); err != nil {
				t.Fatal(err)
			}
			groups := s.Store.CanonicalGroups()
			scope := "cn"
			if platform == provider.Gemini {
				scope = "api"
			}
			s.Reg.ReplaceProvider(platform, scope, []provider.Model{{ID: "glm-5.3"}})
			if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
				t.Fatal(err)
			}
			got := find()
			if got.ID != m.ID || got.Enabled || len(got.Sources) != len(m.Sources)+1 {
				t.Fatalf("unexpected reconciliation: %+v", got)
			}
			for _, old := range m.Sources {
				found := false
				for _, src := range got.Sources {
					if src == old {
						found = true
					}
				}
				if !found {
					t.Fatal("existing source changed")
				}
			}
			if !reflect.DeepEqual(groups, s.Store.CanonicalGroups()) {
				t.Fatal("group permissions changed")
			}
			before := s.Store.UnifiedSnapshot()
			if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, s.Store.UnifiedSnapshot()) {
				t.Fatal("unchanged refresh wrote new revisions")
			}
			reopened, err := storage.Open(s.Store.Path())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, reopened.UnifiedSnapshot()) {
				t.Fatal("sync not durable")
			}

		})
	}
}

func TestGeminiAPIAndCLIAreSeparateSources(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	for _, scope := range []string{"api", "cli"} {
		s.Reg.ReplaceProvider(provider.Gemini, scope, []provider.Model{{ID: "gemini-3-flash"}})
		if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
			t.Fatal(err)
		}
	}
	found := false
	for _, m := range s.Store.UnifiedSnapshot().Models {
		if m.Name != "gemini-3-flash" {
			continue
		}
		found = true
		seen := map[string]bool{}
		for _, src := range m.Sources {
			seen[src.ID] = true
		}
		if len(m.Sources) != 2 || !seen["gemini/api/gemini-3-flash"] || !seen["gemini/cli/gemini-3-flash"] {
			t.Fatal("modes conflated", m.Sources)
		}
	}
	if !found {
		t.Fatal("missing shared model")
	}
}

func TestAntigravityAddsSourceToSharedGemini(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	for _, scope := range []string{"api", "cli"} {
		s.Reg.ReplaceProvider(provider.Gemini, scope, []provider.Model{{ID: "gemini-3-flash"}})
		if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
			t.Fatal(err)
		}
	}
	s.Reg.ReplaceProvider(provider.Antigravity, "global", []provider.Model{{ID: "gemini-3-flash"}})
	if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range s.Store.UnifiedSnapshot().Models {
		if m.Name != "gemini-3-flash" {
			continue
		}
		found = true
		seen := map[string]bool{}
		for _, src := range m.Sources {
			seen[src.ID] = true
		}
		if len(m.Sources) != 3 || !seen["antigravity/global/gemini-3-flash"] || !seen["gemini/api/gemini-3-flash"] || !seen["gemini/cli/gemini-3-flash"] {
			t.Fatal("modes conflated", m.Sources)
		}
	}
	if !found {
		t.Fatal("missing shared model")
	}
}

func TestGrokBuildCatalogCreatesSharedName(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.Grok, "build", []provider.Model{{ID: "grok-4.7"}})
	if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
		t.Fatal(err)
	}
	for _, m := range s.Store.UnifiedSnapshot().Models {
		if m.Name == "grok-4.7" {
			if len(m.Sources) != 1 || m.Sources[0].ID != "grok/build/grok-4.7" {
				t.Fatal(m.Sources)
			}
			return
		}
	}
	t.Fatal("missing shared Grok model")
}
