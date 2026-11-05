package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
)

func legacyModelState() *State {
	return &State{Version: 10, Settings: DefaultSettings(), Groups: []AccountGroup{{ID: "g1", Name: "one", AccountIDs: []string{}, ModelMappings: []ModelMapping{{Name: "deepseek-4.1-flash", Sources: []string{"workbuddy/cn/DeepSeek-4.1", "trae/cn/DeepSeek-4.1"}}}}}}
}
func TestModelMigrationDeterministicAndDoesNotWiden(t *testing.T) {
	st := legacyModelState()
	g := cloneGroup(st.Groups[0])
	g.ID = "g2"
	g.Name = "two"
	g.ModelMappings[0].Sources[0], g.ModelMappings[0].Sources[1] = g.ModelMappings[0].Sources[1], g.ModelMappings[0].Sources[0]
	off := false
	g.ModelRules = []access.ModelRule{{ID: "deepseek-4.1-flash", Visible: &off}, {ID: "workbuddy/cn/DeepSeek-4.1", Callable: &off}}
	g.AllowlistEnabled = true
	g.ModelIDs = []string{}
	st.Groups = append(st.Groups, g)
	before, _ := json.Marshal(st)
	next, r := convertModelState(st)
	if !r.Ready || r.Definitions != 1 {
		t.Fatal(r)
	}
	after, _ := json.Marshal(st)
	if string(before) != string(after) {
		t.Fatal("input mutated")
	}
	if next.Groups[1].UnifiedModels[0].Visible || next.Groups[1].UnifiedModels[0].SourceMode != "selected" || !next.Groups[1].AllowlistEnabled || len(next.Groups[1].ModelIDs) != 0 {
		t.Fatal("permissions widened")
	}
	for _, g := range next.Groups {
		if len(g.ModelMappings) != 0 || g.RawModelMode != "qualified_only" || g.RawModelsVisibleDefault {
			t.Fatal("legacy scope changed")
		}
	}
	twice, r2 := convertModelState(next)
	if !r2.Ready || !reflect.DeepEqual(next, twice) {
		t.Fatal("not idempotent", r2)
	}
	repeat, _ := convertModelState(st)
	if !reflect.DeepEqual(next, repeat) {
		t.Fatal("non-deterministic IDs")
	}
}
func TestModelMigrationConflictCases(t *testing.T) {
	cases := []struct {
		name, reason string
		change       func(*State)
	}{
		{"different-sources", "name_sources_conflict", func(st *State) {
			g := cloneGroup(st.Groups[0])
			g.ID = "g2"
			g.ModelMappings[0].Sources = g.ModelMappings[0].Sources[:1]
			st.Groups = append(st.Groups, g)
		}},
		{"case", "name_case_conflict", func(st *State) {
			g := cloneGroup(st.Groups[0])
			g.ID = "g2"
			g.ModelMappings[0].Name = "DeepSeek-4.1-flash"
			st.Groups = append(st.Groups, g)
		}},
		{"raw-name", "name_namespace_conflict", func(st *State) { st.Groups[0].ModelMappings[0].Name = "deepseek-v4.1-flash" }},
		{"builtin-alias", "name_namespace_conflict", func(st *State) { st.Groups[0].ModelMappings[0].Name = "gpt-5" }},
		{"custom-alias", "name_namespace_conflict", func(st *State) { st.Settings.ModelAliases = map[string]string{"deepseek-4.1-flash": "glm-5.3"} }},
		{"force", "force_model_conflict", func(st *State) { st.Settings.ForceModel = "glm-5.3" }},
		{"default", "default_reference_conflict", func(st *State) { st.Groups[0].DefaultModel = "missing-1" }},
		{"duplicate", "invalid_legacy_mapping", func(st *State) {
			st.Groups[0].ModelMappings[0].Sources = append(st.Groups[0].ModelMappings[0].Sources, "workbuddy/cn/other-1")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := legacyModelState()
			tc.change(st)
			raw, _ := json.Marshal(st)
			path := filepath.Join(t.TempDir(), "state.json")
			os.WriteFile(path, raw, 0600)
			report, err := CheckStateMigration(path)
			if err != nil || report.Ready {
				t.Fatal(report, err)
			}
			found := false
			for _, x := range report.Conflicts {
				found = found || x.Reason == tc.reason
			}
			if !found {
				t.Fatal("reason", report)
			}
			if _, err = Open(path); err == nil {
				t.Fatal("conflicting open accepted")
			}
			after, _ := os.ReadFile(path)
			if string(raw) != string(after) {
				t.Fatal("conflict changed state")
			}
		})
	}
}
func TestModelMigrationPreservesIndependentState(t *testing.T) {
	st := legacyModelState()
	st.ModelPolicies = map[string]ModelPolicy{"raw-1": {ContextWindow: 10000}}
	st.Prices = map[string]ModelPrice{"raw-1": {Currency: "USD"}}
	st.Keys = []*APIKey{{Key: "fixture-secret", GroupID: "g1", BudgetUsage: BudgetUsage{Requests: 7}}}
	st.UsageSequence = 12
	st.Groups[0].ModelMappings = nil
	next, r := convertModelState(st)
	if !r.Ready || next.Groups[0].RawModelMode != "legacy" || !next.Groups[0].RawModelsVisibleDefault {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(st.Keys, next.Keys) || !reflect.DeepEqual(st.ModelPolicies, next.ModelPolicies) || !reflect.DeepEqual(st.Prices, next.Prices) || next.UsageSequence != 12 {
		t.Fatal("non-routing data changed")
	}
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "fixture-secret") {
		t.Fatal("secret in report")
	}
}
func TestModelMigrationOfflineCopyAndGuards(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("offline writer detection is Linux-only")
	}
	root := t.TempDir()
	src := filepath.Join(root, "source")
	os.Mkdir(src, 0700)
	st := legacyModelState()
	raw, _ := json.Marshal(st)
	os.WriteFile(filepath.Join(src, "state.json"), raw, 0600)
	os.WriteFile(filepath.Join(src, "state.json.growth.json"), []byte("fixture-sidecar"), 0600)
	dst := filepath.Join(root, "output")
	report, err := MigrateStateDirectory(src, dst)
	if err != nil || !report.Ready {
		t.Fatal(report, err)
	}
	again, err := Open(filepath.Join(dst, "state.json"))
	if err != nil || len(again.UnifiedSnapshot().Models) != 1 {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(filepath.Join(src, "state.json"))
	if string(raw) != string(original) {
		t.Fatal("input overwritten")
	}
	side, _ := os.ReadFile(filepath.Join(dst, "state.json.growth.json"))
	if string(side) != "fixture-sidecar" {
		t.Fatal("sidecar lost")
	}
	if _, err = MigrateStateDirectory(src, dst); err == nil {
		t.Fatal("overwrote output")
	}
	if _, err = MigrateStateDirectory(src, filepath.Join(src, "nested")); err == nil {
		t.Fatal("nested output")
	}
	os.Symlink(filepath.Join(src, "state.json"), filepath.Join(src, "link"))
	if _, err = MigrateStateDirectory(src, filepath.Join(root, "linked")); err == nil {
		t.Fatal("followed symlink")
	}
	os.WriteFile(filepath.Join(dst, migrationPending), []byte("{}"), 0600)
	if _, err = Open(filepath.Join(dst, "state.json")); err == nil {
		t.Fatal("partial migration opened")
	}
}
func TestUnifiedRevisionExhaustion(t *testing.T) {
	s := testPool(t, 0)
	s.st.ModelRoutingRevision = config.MaxModelRevision
	_, err := s.SaveUnifiedModel(unifiedFixture(), nil, 0, config.MaxModelRevision, nil)
	expectModelReason(t, err, "revision_conflict")
	if len(s.st.UnifiedModels) != 0 {
		t.Fatal("overflow mutated")
	}
}

func TestModelMigrationReplaysJournalExactlyOnce(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux offline migration")
	}
	s, key := journalStore(t)
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	json.Unmarshal(raw, &doc)
	doc["version"] = json.RawMessage(`10`)
	delete(doc, "unified_models")
	delete(doc, "model_routing_revision")
	legacy, _ := json.Marshal(doc)
	os.WriteFile(s.Path(), legacy, 0600)
	s.usageBaseReady = true // Model a running schema-10 process with a durable checkpoint.
	if err = s.Record(journalEntry("migration-ledger"), 2, key); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "converted")
	if _, err = MigrateStateDirectory(filepath.Dir(s.Path()), dest); err != nil {
		t.Fatal(err)
	}
	a, err := Open(filepath.Join(dest, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertSettlementEqual(t, s, a)
	assertSettlementEqual(t, a, reopenJournal(t, a))
	input, _ := os.ReadFile(s.Path())
	if string(input) != string(legacy) {
		t.Fatal("input checkpoint changed")
	}
}

func TestModelMigrationRejectsDamagedUnknownAndFutureState(t *testing.T) {
	for _, raw := range []string{`{"version":10`, `{"version":10,"future_permissions":true}`, `{"version":10,"groups":[{"id":"g","unknown_permission":true}]}`, `{"version":11}`, `{"version":99}`} {
		path := filepath.Join(t.TempDir(), "state.json")
		os.WriteFile(path, []byte(raw), 0600)
		report, err := CheckStateMigration(path)
		if err == nil && report.Ready {
			t.Fatal("damaged state accepted")
		}
		after, _ := os.ReadFile(path)
		if string(after) != raw {
			t.Fatal("invalid state modified")
		}
	}
}
