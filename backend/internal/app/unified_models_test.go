package app_test

import (
	"codearts2api/internal/access"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func TestUnifiedGroupsRetainVirtualSearchACL(t *testing.T) {
	srv, st, _ := managementFixture(t)
	id := "workbuddy/cn/web-search"
	group, err := srv.AccessManagement.SaveUnifiedGroup(access.AccountGroup{Name: "search-acl", AccountIDs: []string{"workbuddy"}, AllowlistEnabled: true, ModelIDs: []string{id}}, 0)
	if err != nil || len(group.ModelIDs) != 1 || group.ModelIDs[0] != id {
		t.Fatal(group, err)
	}
	_, err = srv.UnifiedModels.Save(config.UnifiedModel{Name: "search-1", Enabled: true, Sources: []config.ModelSource{{ID: id, Enabled: true}}}, nil, 0, st.UnifiedSnapshot().Revision)
	if err == nil {
		t.Fatal("virtual search capability accepted as unified model source")
	}
}

func TestUnifiedManagementResolvesOnlyRealSources(t *testing.T) {
	srv, st, _ := managementFixture(t)
	srv.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "Model-4.1"}})
	m := config.UnifiedModel{Name: "shared-4.1", Enabled: true, Sources: []config.ModelSource{{ID: "workbuddy/cn/model-4.1", Enabled: true}}}
	saved, err := srv.UnifiedModels.Save(m, nil, 0, st.UnifiedSnapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Sources[0].ID != "workbuddy/cn/Model-4.1" {
		t.Fatal("source not canonical")
	}
	bad := m
	bad.Name = "other-4.1"
	bad.Sources = []config.ModelSource{{ID: "workbuddy/cn/missing", Enabled: true}}
	if _, err = srv.UnifiedModels.Save(bad, nil, 0, st.UnifiedSnapshot().Revision); err == nil {
		t.Fatal("unknown source accepted")
	}
	srv.Reg.ReplaceProvider(provider.WorkBuddy, "cn", nil)
	saved.Enabled = false
	updated, err := srv.UnifiedModels.Save(saved, nil, saved.Revision, st.UnifiedSnapshot().Revision)
	if err != nil {
		t.Fatal("missing old source could not be disabled", err)
	}
	_, err = srv.UnifiedModels.Save(saved, nil, saved.Revision, st.UnifiedSnapshot().Revision)
	var conflict *config.ModelError
	if !errors.As(err, &conflict) || conflict.Reason != "revision_conflict" {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(st.Path())
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil || doc["version"] != float64(11) {
		t.Fatal("not schema 11")
	}
	if updated.Revision != 2 {
		t.Fatal(updated.Revision)
	}
}
