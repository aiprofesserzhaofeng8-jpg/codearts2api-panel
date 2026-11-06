package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func unifiedAPIFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s := poolServer(t, 0)
	s.UnifiedModels = catalog.NewManagement(s.Store, s.Reg, s.ModelPolicies)
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "verified-v1", Context: 65536, MaxOutput: 8192, ImageInput: "supported", ToolCalling: "supported"}})
	s.Reg.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "verified-v1", Context: 32768, MaxOutput: 4096, ImageInput: "unsupported", ToolCalling: "supported"}})
	markModelsFresh(s)
	return s, s.Store.AddKey("unified-api-fixture").Key
}

func TestUnifiedAPIListProfileAndSixSuccessfulResponses(t *testing.T) {
	s, key, _ := mappedFixture(t, nil)
	p := config.DefaultModelPolicies()["trae/cn/deepseek-v4.1-flash"]
	for _, source := range mappedSources {
		if err := s.Store.SetModelPolicyAndSave(source, p); err != nil {
			t.Fatal(err)
		}
	}
	if ids := modelIDsFromResponse(t, scopedModelRequest(s, key)); len(ids) != 1 || ids[0] != mappedPublic {
		t.Fatal(ids)
	}
	r := httptest.NewRequest("GET", "/v1/client-profile?model="+mappedPublic, nil)
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"`+mappedPublic+`"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	onlyMappedPlatform(s, provider.WorkBuddy)
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			w := responseRequest(s, key, path, mappedBody(path, stream, mappedPublic))
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"`+mappedPublic+`"`) {
				t.Fatal(path, stream, w.Code, w.Body.String())
			}
		}
	}
}

func TestUnifiedAPIPolicyBoundsAndNamespaceConflicts(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	detail := createUnifiedHTTP(t, s, "verified-public-1")
	_, _, p, err := s.Routing.Profile(key, detail.Model.Name, s.ModelPolicies)
	if err != nil {
		t.Fatal(err)
	}
	body := unifiedCreateBody(s, detail.Model.Name)
	body["client_policy"], body["expected_revision"] = p, detail.Model.Revision
	path := "/admin/api/unified-models/" + detail.Model.ID
	w := adminProbeRequest(t, s, "PUT", path, string(mustJSON(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := s.Store.UnifiedSnapshot()
	if _, ok := before.Policies[config.UnifiedPolicyKey(detail.Model.ID)]; !ok {
		t.Fatal("policy not stored under unique key")
	}
	lower := config.ModelPolicy{ContextWindow: 16384, MaxOutputTokens: 4096, ReserveTokens: 8192, TriggerTokens: 8192, KeepRecentTokens: 4096, SummaryMaxTokens: 4096, Source: "fixture"}
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/model-policies", string(mustJSON(map[string]any{"model": "trae/cn/verified-v1", "policy": lower, "expected_routing_revision": before.Revision}))), 409, "model_policy_conflict")
	if !reflect.DeepEqual(before, s.Store.UnifiedSnapshot()) {
		t.Fatal("failed bounds validation published state")
	}
	// Changing the other raw policy still finds Trae's catalog metadata; it must
	// not require artificial persisted copies of every source recommendation.
	p.ContextWindow = 65536
	p.Source = "fixture"
	w = adminProbeRequest(t, s, "POST", "/admin/api/model-policies", string(mustJSON(map[string]any{"model": "workbuddy/cn/verified-v1", "policy": p, "expected_routing_revision": before.Revision})))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/settings", string(mustJSON(map[string]any{"model_aliases": map[string]string{detail.Model.Name: "workbuddy/cn/verified-v1"}, "expected_routing_revision": s.Store.UnifiedSnapshot().Revision}))), 409, "model_name_conflict")
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/settings", string(mustJSON(map[string]any{"model_aliases": map[string]string{"alias-1": "alias-2", "alias-2": "workbuddy/cn/verified-v1"}, "expected_routing_revision": s.Store.UnifiedSnapshot().Revision}))), 409, "model_name_conflict")
	current := s.Store.UnifiedSnapshot()
	body["expected_revision"], body["expected_routing_revision"], body["client_policy"] = current.Models[0].Revision, current.Revision, nil
	w = adminProbeRequest(t, s, "PUT", path, string(mustJSON(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, ok := s.Store.UnifiedSnapshot().Policies[config.UnifiedPolicyKey(detail.Model.ID)]; ok {
		t.Fatal("null did not remove public policy")
	}
}

func TestUnifiedAPIDiskFailureRollsBackDefinitionAndSettings(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	st, err := storage.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	s := newConfiguredServer(st)
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "verified-v1", Context: 65536, MaxOutput: 8192}})
	s.Reg.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "verified-v1", Context: 32768, MaxOutput: 4096}})
	markModelsFresh(s)
	// Create the admin session before making the synthetic state path unwritable.
	_ = fixtureAdminCookie(s)
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	before := st.UnifiedSnapshot()
	if err := os.Rename(statePath, filepath.Join(dir, "before.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	createBody := string(mustJSON(unifiedCreateBody(s, "verified-public-1")))
	w := adminProbeRequest(t, s, "POST", "/admin/api/unified-models", createBody)
	checkModelError(t, w, 500, "model_save_failed")
	if !reflect.DeepEqual(before, st.UnifiedSnapshot()) {
		t.Fatal("failed save published model")
	}
	settings := st.Settings()
	w = adminProbeRequest(t, s, "POST", "/admin/api/settings", string(mustJSON(map[string]any{"default_model": "workbuddy/cn/verified-v1", "max_retries": 7, "expected_routing_revision": before.Revision})))
	checkModelError(t, w, 500, "model_save_failed")
	if !reflect.DeepEqual(before, st.UnifiedSnapshot()) || !reflect.DeepEqual(settings, st.Settings()) {
		t.Fatal("failed combined settings write partially applied")
	}
}
func unifiedCreateBody(s *Server, name string) map[string]any {
	return map[string]any{"name": name, "enabled": true, "sources": []config.ModelSource{{ID: "workbuddy/cn/verified-v1", Enabled: true}, {ID: "trae/cn/verified-v1", Enabled: true}}, "client_policy": nil, "expected_routing_revision": s.Store.UnifiedSnapshot().Revision}
}
func createUnifiedHTTP(t *testing.T, s *Server, name string) catalog.UnifiedDetail {
	t.Helper()
	w := adminProbeRequest(t, s, "POST", "/admin/api/unified-models", string(mustJSON(unifiedCreateBody(s, name))))
	var detail catalog.UnifiedDetail
	if json.Unmarshal(w.Body.Bytes(), &detail) != nil || w.Code != 201 || !config.ValidUnifiedID(detail.Model.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	return detail
}
func checkModelError(t *testing.T, w *httptest.ResponseRecorder, status int, reason string) {
	t.Helper()
	var body struct {
		Error struct {
			Code   int    `json:"code"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != status || body.Error.Code != status || body.Error.Reason != reason {
		t.Fatalf("want %d %s: %d %s", status, reason, w.Code, w.Body.String())
	}
}

func TestUnifiedAPIConsistentListProfileAndNoAccounts(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	detail := createUnifiedHTTP(t, s, "verified-public-1")
	w := scopedModelRequest(s, key)
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &list) != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	count := 0
	for _, row := range list.Data {
		if row["id"] == "workbuddy/cn/verified-v1" || row["id"] == "trae/cn/verified-v1" {
			t.Fatal("duplicate source recommended", row)
		}
		if row["id"] == detail.Model.Name {
			count++
			p := row["client_profile"].(map[string]any)
			if row["provider"] != "unified" || row["owned_by"] != "unified" || row["scope"] != "" || p["model_id"] != detail.Model.ID || p["context_window"] != float64(32768) || p["max_output_tokens"] != float64(4096) || p["supports_tools"] != true || row["supports_images"] != true {
				t.Fatal(row)
			}
			if strings.Contains(string(mustJSON(row)), "workbuddy/") || strings.Contains(string(mustJSON(row)), "trae/") {
				t.Fatal("source details leaked", row)
			}
		}
	}
	if count != 1 {
		t.Fatal("public name not unique", count)
	}
	r := httptest.NewRequest("GET", "/v1/client-profile?model="+detail.Model.Name, nil)
	r.Header.Set("Authorization", "Bearer "+key)
	p := httptest.NewRecorder()
	s.Handler().ServeHTTP(p, r)
	if p.Code != 200 || !strings.Contains(p.Body.String(), `"model_revision":1`) || !strings.Contains(p.Body.String(), `"routing_revision":`) {
		t.Fatal(p.Code, p.Body.String())
	}
	if p.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("profile cached")
	}
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			checkModelError(t, responseRequest(s, key, path, mappedBody(path, stream, detail.Model.Name)), 503, "model_sources_unavailable")
		}
	}
}

func TestUnifiedAPIStrictCRUDRevisionsReferencesAndPagination(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	first := createUnifiedHTTP(t, s, "verified-public-1")
	_ = createUnifiedHTTP(t, s, "verified-public-2")
	w := adminProbeRequest(t, s, "GET", "/admin/api/unified-models?limit=1", "")
	var page struct {
		Models []config.UnifiedModel `json:"models"`
		Next   string                `json:"next_after"`
	}
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || w.Code != 200 || len(page.Models) != 1 || page.Next != page.Models[0].ID {
		t.Fatal(w.Code, w.Body.String())
	}
	w = adminProbeRequest(t, s, "GET", "/admin/api/unified-models?limit=1&after="+page.Next, "")
	var second struct {
		Models []config.UnifiedModel `json:"models"`
		Next   string                `json:"next_after"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &second)
	if len(second.Models) != 1 || second.Next != "" || page.Models[0].ID >= second.Models[0].ID {
		t.Fatal(w.Body.String())
	}
	for _, query := range []string{"?limit=0", "?limit=201", "?limit=x", "?after=bad", "?limit=1&limit=2"} {
		if w := adminProbeRequest(t, s, "GET", "/admin/api/unified-models"+query, ""); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	body := unifiedCreateBody(s, first.Model.Name)
	body["expected_revision"] = first.Model.Revision
	path := "/admin/api/unified-models/" + first.Model.ID
	w = adminProbeRequest(t, s, "PUT", path, string(mustJSON(body)))
	if w.Code != 200 || s.Store.UnifiedSnapshot().Revision != 3 {
		t.Fatal("no-op advanced revision", w.Code, w.Body.String())
	}
	body["name"] = "renamed-1"
	checkModelError(t, adminProbeRequest(t, s, "PUT", path, string(mustJSON(body))), 400, "immutable_model_name")
	body["name"], body["enabled"] = first.Model.Name, false
	w = adminProbeRequest(t, s, "PUT", path, string(mustJSON(body)))
	if w.Code != 200 || s.Store.UnifiedSnapshot().Revision != 4 {
		t.Fatal(w.Code, w.Body.String())
	}
	checkModelError(t, adminProbeRequest(t, s, "PUT", path, string(mustJSON(body))), 409, "revision_conflict")
	body["expected_routing_revision"], body["expected_revision"] = uint64(4), uint64(0)
	checkModelError(t, adminProbeRequest(t, s, "PUT", path, string(mustJSON(body))), 428, "revision_required")
	checkModelError(t, adminProbeRequest(t, s, "DELETE", path, `{}`), 428, "revision_required")
	g, err := s.Store.SaveUnifiedGroup(access.AccountGroup{Name: "reference-fixture", UnifiedModels: []access.UnifiedGrant{{ModelID: first.Model.ID, Visible: true, Callable: true, SourceMode: "all", SourceIDs: []string{}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	w = adminProbeRequest(t, s, "GET", path, "")
	if !strings.Contains(w.Body.String(), `"group_grant"`) || !strings.Contains(w.Body.String(), `"last_probe":null`) || strings.Contains(w.Body.String(), "fixture-admin") {
		t.Fatal(w.Body.String())
	}
	checkModelError(t, adminProbeRequest(t, s, "DELETE", path, `{"expected_revision":2,"expected_routing_revision":4}`), 409, "model_in_use")
	if err := s.Store.DeleteUnifiedGroup(g.ID, g.Revision); err != nil {
		t.Fatal(err)
	}
	w = adminProbeRequest(t, s, "DELETE", path, `{"expected_revision":2,"expected_routing_revision":4}`)
	if w.Code != 200 || s.Store.UnifiedSnapshot().Revision != 5 {
		t.Fatal(w.Code, w.Body.String())
	}
	checkModelError(t, adminProbeRequest(t, s, "GET", path, ""), 404, "model_not_found")
	if len(s.Store.AuditEvents()) == 0 {
		t.Fatal("writes not audited")
	}
}

func TestUnifiedAPIRejectsMalformedAndUnauthorizedWrites(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	before := s.Store.UnifiedSnapshot()
	valid := string(mustJSON(unifiedCreateBody(s, "verified-public-1")))
	for _, bad := range []string{`null`, `[]`, valid + ` {}`, strings.Replace(valid, `"enabled":true`, `"enabled":null`, 1), strings.Replace(valid, `"client_policy":null,`, "", 1), strings.Replace(valid, `"enabled":true`, `"enabled":true,"secret":"no"`, 1), strings.Replace(valid, `"enabled":true`, `"enabled":true,"revision":2`, 1)} {
		w := adminProbeRequest(t, s, "POST", "/admin/api/unified-models", bad)
		if w.Code != 400 || !reflect.DeepEqual(before, s.Store.UnifiedSnapshot()) {
			t.Fatal("bad write accepted/mutated", bad, w.Code, w.Body.String())
		}
	}
	w := adminProbeRequest(t, s, "POST", "/admin/api/unified-models", `{"name":"`+strings.Repeat("x", 33<<10)+`"}`)
	if w.Code != 413 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("POST", "/admin/api/unified-models", strings.NewReader(valid))
	r.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("inference key reached admin", w.Code)
	}
	g, err := s.Store.SaveUnifiedGroup(access.AccountGroup{Name: "user-fixture"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	createTestUser(t, s, "unified-user", g.ID)
	cookie := loginTestUser(t, s, "unified-user")
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		w = userHTTP(s, cookie, method, "/admin/api/unified-models", valid)
		if w.Code != 403 {
			t.Fatal("user reached admin", method, w.Code)
		}
	}
	r = httptest.NewRequest("POST", "/admin/api/unified-models", strings.NewReader(valid))
	r.AddCookie(fixtureAdminCookie(s))
	r.Header.Set("Origin", "https://other.invalid")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin admin write accepted", w.Code)
	}
}

func TestUnifiedAPIGroupCASAndUserProjection(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	m := createUnifiedHTTP(t, s, "verified-public-1").Model
	body := map[string]any{"name": "selected-empty", "account_ids": []string{}, "model_ids": []string{}, "unified_models": []access.UnifiedGrant{{ModelID: m.ID, Visible: true, Callable: false, SourceMode: "selected", SourceIDs: []string{}}}}
	w := adminProbeRequest(t, s, "POST", "/admin/api/groups", string(mustJSON(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	g := s.Store.CanonicalGroups()[0]
	if g.RawModelMode != "qualified_only" || g.RawModelsVisibleDefault || g.ModelMappings != nil {
		t.Fatal(g)
	}
	createTestUser(t, s, "unified-reader", g.ID)
	cookie := loginTestUser(t, s, "unified-reader")
	w = userHTTP(s, cookie, "GET", "/user/api/models", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), m.Name) || strings.Contains(w.Body.String(), "workbuddy/") {
		t.Fatal(w.Code, w.Body.String())
	}
	k, err := s.Store.AddKeyInGroupAndSave("fixture", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ids := modelIDsFromResponse(t, scopedModelRequest(s, k.Key)); len(ids) != 1 || ids[0] != m.Name {
		t.Fatal(ids)
	}
	checkModelError(t, responseRequest(s, k.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, m.Name)), 403, "model_permission_denied")
	body["id"], body["name"] = g.ID, "updated"
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/groups", string(mustJSON(body))), 428, "revision_required")
	body["expected_revision"] = g.Revision
	w = adminProbeRequest(t, s, "POST", "/admin/api/groups", string(mustJSON(body)))
	if w.Code != 200 || strings.Contains(w.Body.String(), "model_mappings") {
		t.Fatal(w.Code, w.Body.String())
	}
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/groups", string(mustJSON(body))), 409, "revision_conflict")
	body["model_mappings"] = nil
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/groups", string(mustJSON(body))), 400, "legacy_mapping_field")
	delete(body, "model_mappings")
	body["unified_models"] = nil
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/groups", string(mustJSON(body))), 400, "invalid_model_definition")
	checkModelError(t, adminProbeRequest(t, s, "DELETE", "/admin/api/groups?id="+g.ID, ""), 428, "revision_required")
}

func TestUnifiedAPIProfileMissingMetadataAndExactNames(t *testing.T) {
	s, key := unifiedAPIFixture(t)
	m := createUnifiedHTTP(t, s, "verified-public-1").Model
	s.Reg.ReplaceProvider(provider.Trae, "cn", nil)
	request := func(name string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/client-profile?model="+name, nil)
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	partial := request(m.Name)
	if partial.Code != 200 || !strings.Contains(partial.Body.String(), `"client_profile_status":"missing_limits"`) || !strings.Contains(partial.Body.String(), `"supports_images":true`) {
		t.Fatal(partial.Code, partial.Body.String())
	}
	w := scopedModelRequest(s, key)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"client_profile_error":{"code":"client_profile_missing"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	checkModelError(t, request(strings.ToUpper(m.Name)), 404, "model_not_found")
	p := config.DefaultModelPolicies()["trae/cn/deepseek-v4.1-flash"]
	if err := s.Store.SetModelPolicyAndSave("trae/cn/verified-v1", p); err != nil {
		t.Fatal(err)
	}
	w = request(m.Name)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"supports_images":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// A stale explicit client preference must report its failure, not pick a raw model.
	g, err := s.Store.SaveUnifiedGroup(access.AccountGroup{Name: "default-fixture", ClientDefaultModel: m.Name, UnifiedModels: []access.UnifiedGrant{{ModelID: m.ID, Visible: true, Callable: true, SourceMode: "all", SourceIDs: []string{}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddKeyInGroupAndSave("preference", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := s.Store.UnifiedSnapshot()
	m = snapshot.Models[0]
	m.Enabled = false
	if _, err := s.Store.SaveUnifiedModel(m, nil, m.Revision, snapshot.Revision, nil); err != nil {
		t.Fatal(err)
	}
	key = k.Key
	checkModelError(t, request(""), 503, "model_sources_unavailable")
}

func TestUnifiedAPIRoutingSettingsAndRawPolicyCAS(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	m := createUnifiedHTTP(t, s, "verified-public-1")
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/settings", `{"default_model":"verified-public-1"}`), 428, "revision_required")
	w := adminProbeRequest(t, s, "POST", "/admin/api/settings", fmt.Sprintf(`{"default_model":"verified-public-1","expected_routing_revision":%d}`, m.RoutingRevision))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/settings", fmt.Sprintf(`{"force_model":"test-model","expected_routing_revision":%d}`, s.Store.UnifiedSnapshot().Revision)), 409, "model_name_conflict")
	p := config.DefaultModelPolicies()["trae/cn/deepseek-v4.1-flash"]
	body := map[string]any{"model": "trae/cn/verified-v1", "policy": p}
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/model-policies", string(mustJSON(body))), 428, "revision_required")
	body["expected_routing_revision"] = s.Store.UnifiedSnapshot().Revision
	w = adminProbeRequest(t, s, "POST", "/admin/api/model-policies", string(mustJSON(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	checkModelError(t, adminProbeRequest(t, s, "POST", "/admin/api/model-policies", string(mustJSON(body))), 409, "revision_conflict")
	if w := adminProbeRequest(t, s, "POST", "/admin/api/settings", `{"max_retries":2}`); w.Code != 200 {
		t.Fatal("unrelated setting requires routing revision", w.Code, w.Body.String())
	}
}

func TestUnifiedAPISelectedPolicyAndIndependentRawVisibility(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	m := createUnifiedHTTP(t, s, "verified-public-1").Model
	s.Store.UpsertAccount(&accounts.Account{ID: "wb-fixture", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true})
	g, err := s.Store.SaveUnifiedGroup(access.AccountGroup{Name: "selected-fixture", AccountIDs: []string{"wb-fixture"}, UnifiedModels: []access.UnifiedGrant{{ModelID: m.ID, Visible: true, Callable: true, SourceMode: "selected", SourceIDs: []string{"workbuddy/cn/verified-v1"}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddKeyInGroupAndSave("selected", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/v1/client-profile?model="+m.Name, nil)
	r.Header.Set("Authorization", "Bearer "+k.Key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"context_window":65536`) || !strings.Contains(w.Body.String(), `"supports_images":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if ids := modelIDsFromResponse(t, scopedModelRequest(s, k.Key)); !reflect.DeepEqual(ids, []string{m.Name}) {
		t.Fatal(ids)
	}
	visible := true
	callable := false
	g.ModelRules = []access.ModelRule{{ID: "workbuddy/cn/verified-v1", Visible: &visible}}
	g, err = s.Store.SaveUnifiedGroup(g, g.Revision)
	if err != nil {
		t.Fatal(err)
	}
	ids := modelIDsFromResponse(t, scopedModelRequest(s, k.Key))
	if len(ids) != 2 || ids[0] != m.Name || ids[1] != "workbuddy/cn/verified-v1" {
		t.Fatal(ids)
	}
	g.ModelRules[0].Callable = &callable
	if _, err = s.Store.SaveUnifiedGroup(g, g.Revision); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	checkModelError(t, w, 403, "model_permission_denied")
	if ids := modelIDsFromResponse(t, scopedModelRequest(s, k.Key)); len(ids) != 2 {
		t.Fatal("callable suppressed visible model", ids)
	}
}

func TestUnifiedAPIProtocolReasonsAndUpstreamIsolation(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			s, key, _ := mappedFixture(t, nil)
			for _, stream := range []bool{false, true} {
				checkModelError(t, responseRequest(s, key, path, mappedBody(path, stream, "unknown-public-1")), 404, "model_not_found")
			}
			g := s.Store.CanonicalGroups()[0]
			g.UnifiedModels[0].Callable = false
			if _, err := s.Store.SaveUnifiedGroup(g, g.Revision); err != nil {
				t.Fatal(err)
			}
			checkModelError(t, responseRequest(s, key, path, mappedBody(path, false, mappedPublic)), 403, "model_permission_denied")
			s, key, _ = mappedFixture(t, func(_ string, w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"error":{"message":"fixture upstream denied"}}`))
			})
			onlyMappedPlatform(s, provider.WorkBuddy)
			w := responseRequest(s, key, path, mappedBody(path, false, mappedPublic))
			if w.Code != 502 || strings.Contains(w.Body.String(), `"reason":"model_permission_denied"`) {
				t.Fatal("upstream error relabeled", w.Code, w.Body.String())
			}
		})
	}
	s, key := unifiedAPIFixture(t)
	m := createUnifiedHTTP(t, s, "verified-public-1").Model
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "verified-v1", ToolCalling: "unsupported"}})
	s.Reg.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "verified-v1", ToolCalling: "unsupported"}})
	body := `{"model":"` + m.Name + `","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}]}`
	checkModelError(t, responseRequest(s, key, "/v1/chat/completions", body), 400, "model_capability_unavailable")
}
