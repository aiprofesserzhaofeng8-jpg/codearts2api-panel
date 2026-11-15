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
