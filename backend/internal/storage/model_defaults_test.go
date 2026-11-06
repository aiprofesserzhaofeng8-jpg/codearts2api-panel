package storage

import (
	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultModelsFailureRollsBackEntireBatch(t *testing.T) {
	s := testPool(t, 0)
	before := s.UnifiedSnapshot()
	bytes, _ := os.ReadFile(s.Path())
	good := config.UnifiedModel{Name: "deepseek-v4.1-flash", Enabled: true, Sources: []config.ModelSource{{ID: "codearts/deepseek-v4.1-flash", Enabled: true}}}
	bad := config.CloneUnifiedModel(good)
	bad.Name = "bad/name"
	if _, err := s.ApplyDefaultModels([]config.UnifiedModel{good, bad}, before.Revision, "", 0, nil); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if !reflect.DeepEqual(before, s.UnifiedSnapshot()) {
		t.Fatal("partial models retained")
	}
	path := s.path
	block := filepath.Join(t.TempDir(), "file")
	os.WriteFile(block, []byte("fixture"), 0600)
	s.path = filepath.Join(block, "state.json")
	if _, err := s.ApplyDefaultModels([]config.UnifiedModel{good}, before.Revision, "", 0, nil); err == nil {
		t.Fatal("disk failure ignored")
	}
	if !reflect.DeepEqual(before, s.UnifiedSnapshot()) {
		t.Fatal("disk failure mutated config")
	}
	s.path = path
	after, _ := os.ReadFile(path)
	if string(bytes) != string(after) {
		t.Fatal("failed batch wrote disk")
	}
}
func TestNativeNameDefinitionAndLegacyGroupDefaultsSurviveRestart(t *testing.T) {
	s := testPool(t, 1)
	_, err := s.SaveUnifiedGroup(AccountGroup{Name: "legacy", AccountIDs: []string{"0"}, RawModelMode: "legacy", DefaultModel: "glm-5.3", ClientDefaultModel: "glm-5.3", RawModelsVisibleDefault: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := config.UnifiedModel{Name: "glm-5.3", Enabled: true, Sources: []config.ModelSource{{ID: "codearts/glm-5.3", Enabled: true}}}
	if _, err = s.ApplyDefaultModels([]config.UnifiedModel{m}, s.UnifiedSnapshot().Revision, "", 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(s.Path()); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultModelsExistingGrantAndStaleGroupRemainProtected(t *testing.T) {
	s := testPool(t, 1)
	m := config.UnifiedModel{Name: "glm-5.3", Enabled: true, Sources: []config.ModelSource{{ID: "codearts/glm-5.3", Enabled: true}}}
	m, err := s.SaveUnifiedModel(m, nil, 0, s.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	group, err := s.SaveUnifiedGroup(AccountGroup{Name: "restricted", AccountIDs: []string{"0"}, UnifiedModels: []access.UnifiedGrant{{ModelID: m.ID, Visible: false, Callable: false, SourceMode: "selected", SourceIDs: []string{}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	before := s.CanonicalGroups()
	if _, err = s.ApplyDefaultModels([]config.UnifiedModel{m}, s.UnifiedSnapshot().Revision, group.ID, group.Revision, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.CanonicalGroups()) {
		t.Fatal("explicit existing denial widened")
	}
	group.Name = "edited"
	if _, err = s.SaveUnifiedGroup(group, group.Revision); err != nil {
		t.Fatal(err)
	}
	snap := s.UnifiedSnapshot()
	other := config.UnifiedModel{Name: "qoder-auto", Enabled: true, Sources: []config.ModelSource{{ID: "qoder/cn/auto", Enabled: true}}}
	if _, err = s.ApplyDefaultModels([]config.UnifiedModel{other}, snap.Revision, group.ID, group.Revision, nil); err == nil {
		t.Fatal("stale group allowed")
	}
	if !reflect.DeepEqual(snap, s.UnifiedSnapshot()) {
		t.Fatal("stale group partially wrote model")
	}
}

func TestDefaultModelsPreserveExistingRawAliasTargets(t *testing.T) {
	s := testPool(t, 0)
	if err := s.UpdateSettingsChecked(s.UnifiedSnapshot().Revision, func(c *Settings) { c.ModelAliases = map[string]string{"my-deepseek": "deepseek-v4.1-flash"} }); err != nil {
		t.Fatal(err)
	}
	m := config.UnifiedModel{Name: "deepseek-v4.1-flash", Enabled: true, Sources: []config.ModelSource{{ID: "codearts/deepseek-v4.1-flash", Enabled: true}}}
	if _, err := s.ApplyDefaultModels([]config.UnifiedModel{m}, s.UnifiedSnapshot().Revision, "", 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSettingsChecked(s.UnifiedSnapshot().Revision, func(c *Settings) { c.DefaultModel = "deepseek-v4.1-flash" }); err != nil {
		t.Fatal("existing raw alias blocked settings", err)
	}
	if err := s.UpdateSettingsChecked(s.UnifiedSnapshot().Revision, func(c *Settings) { c.ModelAliases["deepseek-v4.1-flash"] = "glm-5.3" }); err == nil {
		t.Fatal("public name shadowed")
	}
}
