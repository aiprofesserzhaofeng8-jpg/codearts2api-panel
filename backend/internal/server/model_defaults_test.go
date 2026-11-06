package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func defaultPreview(t *testing.T, s *Server) catalog.DefaultModelsPreview {
	t.Helper()
	w := adminProbeRequest(t, s, "GET", "/admin/api/model-defaults", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p catalog.DefaultModelsPreview
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func defaultBody(p catalog.DefaultModelsPreview, names ...string) config.DefaultModelsApply {
	return config.DefaultModelsApply{Version: p.Version, CatalogRevision: p.CatalogRevision, ExpectedRoutingRevision: p.RoutingRevision, Names: names}
}
func TestDefaultModelsAllProvidersAtomicIdempotentAndReopen(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.CodeArts, "", []provider.Model{{ID: "deepseek-v4.1-flash"}})
	for _, p := range []string{provider.WorkBuddy, provider.Trae, provider.Qoder, provider.Kiro, provider.ChatGPT, provider.Claude, provider.TeleAgent} {
		id := "native-v1"
		if p == provider.WorkBuddy || p == provider.Trae {
			id = "deepseek-v4.1-flash"
		}
		if p == provider.Qoder {
			id = "auto"
		}
		s.Reg.ReplaceProvider(p, "cn", []provider.Model{{ID: id}})
	}
	preview := defaultPreview(t, s)
	if len(preview.Providers) != 8 {
		t.Fatal(preview)
	}
	names := []string{}
	var deep catalog.DefaultModelEntry
	for _, row := range preview.Models {
		if row.Status == "conflict" {
			t.Fatal(row)
		}
		names = append(names, row.Name)
		if row.Name == "deepseek-v4.1-flash" {
			deep = row
		}
	}
	if len(deep.Sources) != 3 {
		t.Fatal("verified sources not grouped", deep)
	}
	request := defaultBody(preview, names...)
	w := adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(request)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	snap := s.Store.UnifiedSnapshot()
	if len(snap.Models) != len(names) {
		t.Fatal(snap)
	}
	again := defaultPreview(t, s)
	for _, row := range again.Models {
		if row.Status != "unchanged" {
			t.Fatal(row)
		}
	}
	w = adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(defaultBody(again, names...))))
	if w.Code != 200 || !reflect.DeepEqual(snap, s.Store.UnifiedSnapshot()) {
		t.Fatal("repeat changed state", w.Body.String())
	}
	reopened, err := storage.Open(s.Store.Path())
	if err != nil || !reflect.DeepEqual(snap, reopened.UnifiedSnapshot()) {
		t.Fatal("new public names did not reopen", err)
	}
	// Native names remain directly addressable, distinct from the new public route.
	key, _ := s.Store.AddKeyInGroupAndSave("native-collision", "")
	_, plan, err := s.Routing.Resolve(key.Key, s.Store.Settings(), deep.Name)
	if err != nil || plan == nil || len(plan.IDs) != 3 {
		t.Fatal(plan, err)
	}
	_, plan, err = s.Routing.Resolve(key.Key, s.Store.Settings(), "codearts/deepseek-v4.1-flash")
	if err != nil || plan != nil {
		t.Fatal("qualified raw lost", err)
	}
}
func TestDefaultModelsPreviewStaleAndMalformedRejectWithoutWrites(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	p := defaultPreview(t, s)
	before := s.Store.UnifiedSnapshot()
	s.Reg.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "auto"}})
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(defaultBody(p, p.Models[0].Name)))), 409, "revision_conflict")
	if !reflect.DeepEqual(before, s.Store.UnifiedSnapshot()) {
		t.Fatal("stale preview wrote")
	}
	p = defaultPreview(t, s)
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(defaultBody(p, "not-in-preview")))), 400, "invalid_model_definition")
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", `{}`), 400, "invalid_model_definition")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/admin/api/model-defaults", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestDefaultModelsLegacyReuseAndGroupPermissions(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "deepseek-v4.1-flash"}})
	old, err := s.UnifiedModels.Save(config.UnifiedModel{Name: "deepseek-4.1-flash", Enabled: false, Sources: []config.ModelSource{{ID: "workbuddy/cn/deepseek-v4.1-flash", Enabled: false}}}, nil, 0, s.Store.UnifiedSnapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	s.Store.UpsertAccount(&accounts.Account{ID: "default-wb", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true})
	group, err := s.Store.SaveUnifiedGroup(storage.AccountGroup{Name: "default-group", AccountIDs: []string{"default-wb"}, AllowlistEnabled: true, ModelIDs: []string{"workbuddy/cn/deepseek-v4.1-flash"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	preview := defaultPreview(t, s)
	request := defaultBody(preview, old.Name)
	request.GroupID = group.ID
	request.ExpectedGroupRevision = group.Revision
	w := adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(request)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	snap := s.Store.UnifiedSnapshot()
	if len(snap.Models) != 1 || snap.Models[0].ID != old.ID || snap.Models[0].Enabled || snap.Models[0].Sources[0].Enabled {
		t.Fatal("old config overwritten", snap.Models)
	}
	if len(s.Store.CanonicalGroups()[0].UnifiedModels) != 0 {
		t.Fatal("disabled model granted")
	}
	current := snap.Models[0]
	current.Enabled = true
	current.Sources[0].Enabled = true
	_, err = s.Store.SaveUnifiedModel(current, nil, current.Revision, snap.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	preview = defaultPreview(t, s)
	request = defaultBody(preview, old.Name)
	request.GroupID = group.ID
	request.ExpectedGroupRevision = group.Revision
	w = adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(request)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	grants := s.Store.CanonicalGroups()[0].UnifiedModels
	if len(grants) != 1 || !reflect.DeepEqual(grants[0].SourceIDs, []string{"workbuddy/cn/deepseek-v4.1-flash"}) {
		t.Fatal("grant escaped group ACL", fmt.Sprint(grants))
	}
}

func TestDefaultV2ConsolidatesExistingProviderNamesIntoOnePublicModel(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	for _, p := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		scope := "cn"
		if p == provider.CodeArts {
			scope = ""
		}
		s.Reg.ReplaceProvider(p, scope, []provider.Model{{ID: "glm-5.3-flash", Context: 65536, MaxOutput: 8192}})
		source := config.CanonicalSource(provider.ModelID(p, scope, "glm-5.3-flash"))
		_, err := s.UnifiedModels.Save(config.UnifiedModel{Name: p + "-glm-5.3-flash", Enabled: true, Sources: []config.ModelSource{{ID: source, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	p := defaultPreview(t, s)
	var name string
	for _, entry := range p.Models {
		if entry.Name == "glm-5.3-flash" {
			name = entry.Name
			if len(entry.Sources) != 3 {
				t.Fatal(entry)
			}
		}
	}
	if name == "" {
		t.Fatal("missing canonical model")
	}
	w := adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(defaultBody(p, name))))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	entries, err := s.Routing.ClientModels(key, s.ModelPolicies)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Model.Provider == "unified" {
			count++
			if entry.Model.ID != name {
				t.Fatal("duplicate public entry", entry.Model.ID)
			}
		}
	}
	if count != 1 {
		t.Fatal("public list count", count)
	}
	_, plan, err := s.Routing.Resolve(key, s.Store.Settings(), name)
	if err != nil || plan == nil || len(plan.IDs) != 3 {
		t.Fatal(plan, err)
	}
	for _, providerName := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		_, oldPlan, e := s.Routing.Resolve(key, s.Store.Settings(), providerName+"-glm-5.3-flash")
		if e != nil || oldPlan == nil || len(oldPlan.IDs) != 1 {
			t.Fatal("old name broken", e)
		}
	}
}

func TestDefaultV2DoesNotReactivateDisabledProviderSource(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "mimo-v2.6-pro"}})
	old, err := s.UnifiedModels.Save(config.UnifiedModel{Name: "workbuddy-mimo-v2.6-pro", Enabled: false, Sources: []config.ModelSource{{ID: "workbuddy/cn/mimo-v2.6-pro", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	p := defaultPreview(t, s)
	w := adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(defaultBody(p, "mimo-v2.6-pro"))))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, m := range s.Store.UnifiedSnapshot().Models {
		if m.ID == old.ID && m.Enabled {
			t.Fatal("old disabled model reactivated")
		}
		if m.Name == "mimo-v2.6-pro" && m.Sources[0].Enabled {
			t.Fatal("disabled source reactivated by consolidation")
		}
	}
}

func TestQoderAliasJoinsCanonicalAndPreservesLegacyCalls(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gfmodel", Name: "GLM-5.3-Flash"}, {ID: "fast", Name: "Fast"}})
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "glm-5.3-flash"}})
	_, err := s.UnifiedModels.Save(config.UnifiedModel{Name: "qoder-gfmodel", Enabled: true, Sources: []config.ModelSource{{ID: "qoder/cn/gfmodel", Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	p := defaultPreview(t, s)
	w := adminProbeRequest(t, s, "POST", "/admin/api/model-defaults", string(mustJSON(defaultBody(p, "glm-5.3-flash", "qoder-fast"))))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	_, plan, err := s.Routing.Resolve(key, s.Store.Settings(), "glm-5.3-flash")
	if err != nil || plan == nil || len(plan.IDs) != 2 {
		t.Fatal(plan, err)
	}
	_, old, err := s.Routing.Resolve(key, s.Store.Settings(), "qoder-gfmodel")
	if err != nil || old == nil || old.IDs[0] != "qoder/cn/gfmodel" {
		t.Fatal(old, err)
	}
	entries, err := s.Routing.ClientModels(key, s.ModelPolicies)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Model.ID == "qoder-gfmodel" {
			t.Fatal("duplicate alias exposed")
		}
	}
	s.Reg.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gfmodel", Name: "GLM-6"}})
	p = defaultPreview(t, s)
	for _, entry := range p.Models {
		if entry.Name == "qoder-gfmodel" && entry.Status == "conflict" {
			return
		}
	}
	// Existing explicit bindings are preserved; the new catalog must not introduce
	// this changed slot as a fresh source of another standard model.
	for _, entry := range p.Models {
		if entry.Name == "glm-6" {
			t.Fatal("catalog drift auto remapped opaque slot")
		}
	}
}
