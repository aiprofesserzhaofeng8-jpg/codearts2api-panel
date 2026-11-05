package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
)

func unifiedFixture() config.UnifiedModel {
	return config.UnifiedModel{Name: "deepseek-4.1-flash", Enabled: true, Sources: []config.ModelSource{{ID: "workbuddy/cn/DeepSeek-4.1", Enabled: true}, {ID: "trae/cn/DeepSeek-4.1", Enabled: true}}}
}
func expectModelReason(t *testing.T, err error, reason string) {
	t.Helper()
	var e *config.ModelError
	if !errors.As(err, &e) || e.Reason != reason {
		t.Fatalf("want %s: %v", reason, err)
	}
}
func createUnified(t *testing.T, s *Store) config.UnifiedModel {
	t.Helper()
	m, err := s.SaveUnifiedModel(unifiedFixture(), nil, 0, s.UnifiedSnapshot().Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestUnifiedCRUDRevisionPersistenceAndIsolation(t *testing.T) {
	s := testPool(t, 0)
	m := createUnified(t, s)
	snapshot := s.UnifiedSnapshot()
	snapshot.Models[0].Sources[0].ID = "corrupted"
	if s.UnifiedSnapshot().Models[0].Sources[0].ID == "corrupted" {
		t.Fatal("mutable snapshot")
	}
	if _, err := s.SaveUnifiedModel(m, nil, m.Revision, snapshot.Revision, nil); err != nil {
		t.Fatal(err)
	}
	if s.UnifiedSnapshot().Revision != snapshot.Revision {
		t.Fatal("no-op bumped revision")
	}
	m.Enabled = false
	changed, err := s.SaveUnifiedModel(m, nil, m.Revision, snapshot.Revision, nil)
	if err != nil || changed.Revision != 2 {
		t.Fatal(changed, err)
	}
	_, err = s.SaveUnifiedModel(m, nil, m.Revision, snapshot.Revision, nil)
	expectModelReason(t, err, "revision_conflict")
	reopened, err := Open(s.Path())
	if err != nil || !reflect.DeepEqual(s.UnifiedSnapshot(), reopened.UnifiedSnapshot()) {
		t.Fatal("roundtrip", err)
	}
	if err = s.DeleteUnifiedModel(m.ID, changed.Revision, s.UnifiedSnapshot().Revision); err != nil {
		t.Fatal(err)
	}
	if len(s.UnifiedSnapshot().Models) != 0 {
		t.Fatal("delete failed")
	}
}
func TestUnifiedConcurrentEditsHaveOneWinner(t *testing.T) {
	s := testPool(t, 0)
	m := createUnified(t, s)
	rev := s.UnifiedSnapshot().Revision
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := config.CloneUnifiedModel(m)
			copy.Enabled = false
			_, err := s.SaveUnifiedModel(copy, nil, m.Revision, rev, nil)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		} else {
			expectModelReason(t, err, "revision_conflict")
		}
	}
	if ok != 1 {
		t.Fatal("lost-update protection failed", ok)
	}
}
func TestUnifiedFailureRollsBackAllFields(t *testing.T) {
	s := testPool(t, 0)
	m := createUnified(t, s)
	before := s.UnifiedSnapshot()
	raw, _ := os.ReadFile(s.Path())
	path := s.path
	block := filepath.Join(t.TempDir(), "blocked")
	os.WriteFile(block, []byte("fixture"), 0600)
	s.path = filepath.Join(block, "state.json")
	m.Enabled = false
	_, err := s.SaveUnifiedModel(m, nil, m.Revision, before.Revision, nil)
	expectModelReason(t, err, "model_save_failed")
	if !reflect.DeepEqual(before, s.UnifiedSnapshot()) {
		t.Fatal("failed write published")
	}
	s.path = path
	after, _ := os.ReadFile(path)
	if string(raw) != string(after) {
		t.Fatal("failed write touched disk")
	}
}
func TestUnifiedPermissionsReferencesAndEmptySelection(t *testing.T) {
	s := testPool(t, 0)
	m := createUnified(t, s)
	grant := access.UnifiedGrant{ModelID: m.ID, Visible: true, Callable: false, SourceMode: "selected", SourceIDs: []string{m.Sources[0].ID}}
	g, err := s.SaveUnifiedGroup(AccountGroup{Name: "restricted", UnifiedModels: []access.UnifiedGrant{grant}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = s.DeleteUnifiedModel(m.ID, m.Revision, s.UnifiedSnapshot().Revision)
	expectModelReason(t, err, "model_in_use")
	copy := config.CloneUnifiedModel(m)
	copy.Sources = copy.Sources[1:]
	_, err = s.SaveUnifiedModel(copy, nil, m.Revision, s.UnifiedSnapshot().Revision, nil)
	expectModelReason(t, err, "model_in_use")
	g.ModelMappings = nil
	g.ModelRules = nil
	g.UnifiedModels[0].SourceIDs = []string{}
	g.UnifiedModels[0].Callable = true
	updated, err := s.SaveUnifiedGroup(g, g.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveUnifiedGroup(g, g.Revision)
	expectModelReason(t, err, "revision_conflict")
	key, _ := s.AddKeyInGroupAndSave("fixture", g.ID)
	scope, _ := s.AccessScope(key.Key)
	sources, ok := scope.Mapping(m.Name)
	if !ok || len(sources) != 0 {
		t.Fatal("empty grant widened", sources)
	}
	newSource := config.ModelSource{ID: "kiro/us-east-1/deepseek-4.1", Enabled: true}
	copy = config.CloneUnifiedModel(m)
	copy.Sources = append(copy.Sources, newSource)
	if _, err = s.SaveUnifiedModel(copy, nil, m.Revision, s.UnifiedSnapshot().Revision, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.Groups()[0].UnifiedModels[0].SourceIDs) != 0 {
		t.Fatal("new source granted implicitly")
	}
	updated.ModelMappings = nil
	updated.ModelRules = nil
	updated.UnifiedModels[0].SourceMode = "all"
	if _, err = s.SaveUnifiedGroup(updated, updated.Revision); err != nil {
		t.Fatal(err)
	}
	scope, _ = s.AccessScope(key.Key)
	sources, _ = scope.Mapping(m.Name)
	if len(sources) != 3 {
		t.Fatal("explicit all not projected", sources)
	}
}
func TestUnifiedValidationAndPolicyBounds(t *testing.T) {
	s := testPool(t, 0)
	m := unifiedFixture()
	for _, name := range []string{"deepseek", "deepseek/v4", "deepseek 4", "glm-5.3", "gpt-5"} {
		m.Name = name
		if _, err := s.SaveUnifiedModel(m, nil, 0, s.UnifiedSnapshot().Revision, nil); err == nil {
			t.Fatal("invalid/conflicting name", name)
		}
	}
	m = unifiedFixture()
	m.Sources = append(m.Sources, m.Sources[0])
	if _, err := s.SaveUnifiedModel(m, nil, 0, s.UnifiedSnapshot().Revision, nil); err == nil {
		t.Fatal("duplicate partition accepted")
	}
	m = unifiedFixture()
	p := DefaultModelPolicies()["deepseek-v4.1-flash"]
	p.SummaryEnabled = false
	p.SummaryRetry = false
	p.DisableSummaryThinking = false
	_, err := s.SaveUnifiedModel(m, &p, 0, s.UnifiedSnapshot().Revision, nil)
	expectModelReason(t, err, "model_policy_conflict")
	policies := map[string]ModelPolicy{}
	for _, src := range m.Sources {
		policies[src.ID] = p
	}
	created, err := s.SaveUnifiedModel(m, &p, 0, s.UnifiedSnapshot().Revision, policies)
	if err != nil {
		t.Fatal(err)
	}
	created.Name = "renamed-4.1"
	_, err = s.SaveUnifiedModel(created, &p, created.Revision, s.UnifiedSnapshot().Revision, policies)
	expectModelReason(t, err, "immutable_model_name")
	err = s.UpdateSettingsChecked(s.UnifiedSnapshot().Revision, func(c *Settings) { c.ForceModel = "glm-5.3" })
	expectModelReason(t, err, "model_name_conflict")
	err = s.UpdateSettings(func(c *Settings) { c.ForceModel = "glm-5.3" })
	expectModelReason(t, err, "model_name_conflict")
}
func TestUnifiedNewStateRejectsMissingPermissionFields(t *testing.T) {
	s := testPool(t, 0)
	m := createUnified(t, s)
	_, err := s.SaveUnifiedGroup(AccountGroup{Name: "g", UnifiedModels: []access.UnifiedGrant{{ModelID: m.ID, SourceMode: "selected", SourceIDs: []string{}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path())
	var base map[string]json.RawMessage
	json.Unmarshal(raw, &base)
	for _, field := range []string{"unified_models", "model_routing_revision"} {
		t.Run(field, func(t *testing.T) {
			doc := map[string]json.RawMessage{}
			for k, v := range base {
				doc[k] = v
			}
			delete(doc, field)
			broken, _ := json.Marshal(doc)
			var st State
			if json.Unmarshal(broken, &st) == nil {
				t.Fatal("missing field accepted")
			}
		})
	}
	for _, needle := range []string{`"callable": false,`, `"visible": false,`, `"raw_models_visible_default": false,`} {
		broken := strings.Replace(string(raw), needle, "", 1)
		if broken == string(raw) {
			t.Fatal("fixture missing", needle)
		}
		var st State
		if json.Unmarshal([]byte(broken), &st) == nil {
			t.Fatal("missing permission accepted", needle)
		}
	}
}

func TestUnifiedSettingsAndPolicyCASAndRollback(t *testing.T) {
	s := testPool(t, 0)
	revision := s.UnifiedSnapshot().Revision
	if err := s.UpdateSettingsChecked(revision, func(c *Settings) { c.ModelAliases["alias-1"] = "glm-5.3" }); err != nil {
		t.Fatal(err)
	}
	err := s.UpdateSettingsChecked(revision, func(c *Settings) { c.DefaultModel = "wrong-1" })
	expectModelReason(t, err, "revision_conflict")
	p := DefaultModelPolicies()["deepseek-v4.1-flash"]
	if err = s.SetModelPolicyChecked("deepseek-v4.1-flash", p, s.UnifiedSnapshot().Revision); err != nil {
		t.Fatal(err)
	}
	err = s.SetModelPolicyChecked("deepseek-v4.1-flash", p, revision)
	expectModelReason(t, err, "revision_conflict")
	before := s.UnifiedSnapshot()
	settings := s.Settings()
	path := s.path
	block := filepath.Join(t.TempDir(), "file")
	os.WriteFile(block, []byte("block"), 0600)
	s.path = filepath.Join(block, "state.json")
	err = s.UpdateSettingsChecked(before.Revision, func(c *Settings) { c.ModelAliases["new-2"] = "glm-5.3" })
	expectModelReason(t, err, "model_save_failed")
	if !reflect.DeepEqual(before, s.UnifiedSnapshot()) || !reflect.DeepEqual(settings, s.Settings()) {
		t.Fatal("settings write failure published")
	}
	s.path = path
}

func TestUnifiedGroupCASMissingAndDelete(t *testing.T) {
	s := testPool(t, 0)
	g, err := s.SaveUnifiedGroup(AccountGroup{Name: "fixture"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	g.ModelMappings = nil
	_, err = s.SaveUnifiedGroup(g, 0)
	expectModelReason(t, err, "revision_required")
	err = s.DeleteUnifiedGroup(g.ID, g.Revision+1)
	expectModelReason(t, err, "revision_conflict")
	if err = s.DeleteUnifiedGroup(g.ID, g.Revision); err != nil {
		t.Fatal(err)
	}
}
