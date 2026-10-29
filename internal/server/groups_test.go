package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

func scopedModelRequest(s *Server, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/v1/models", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func modelIDsFromResponse(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, m := range result.Data {
		out = append(out, m.ID)
	}
	return out
}
func TestGroupModelsAndThreeProtocolsShareScope(t *testing.T) {
	calls := 0
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, summaryFixture("GROUP_OK", "stop", 10, 4))
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	s.Store.UpsertAccount(&codearts.Account{ID: "outside", Provider: "trae", Scope: "cn", ExternalID: "other", Enabled: true})
	s.Reg.ReplaceProvider("workbuddy", "cn", []provider.Model{{ID: "shared", StreamOnly: true}, {ID: "blocked", StreamOnly: true}, {ID: "shared", StreamOnly: true}})
	s.Reg.ReplaceProvider("trae", "cn", []provider.Model{{ID: "shared", StreamOnly: true}})
	s.cache.loadedAt = time.Now()
	g, err := s.Store.SaveGroup(store.AccountGroup{Name: "wb", AccountIDs: []string{"wb-cn"}, AllowlistEnabled: true, ModelIDs: []string{"workbuddy/cn/shared"}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddKeyInGroupAndSave("restricted", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := modelIDsFromResponse(t, scopedModelRequest(s, k.Key))
	if len(ids) != 1 || ids[0] != "workbuddy/cn/shared" {
		t.Fatal("catalog not scoped/deduplicated", ids)
	}
	s.Store.UpdateSettings(func(st *store.Settings) { st.ModelAliases["bypass"] = "workbuddy/cn/blocked" })
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/responses", "/v1/messages"} {
		for _, model := range []string{"workbuddy/cn/blocked", "trae/cn/shared", "bypass"} {
			body := map[string]any{"model": model, "max_tokens": 32, "messages": []map[string]string{{"role": "user", "content": "hi"}}}
			if path == "/v1/responses" || path == "/responses" {
				delete(body, "messages")
				body["input"] = "hi"
			}
			w := responseRequest(s, k.Key, path, string(mustJSON(body)))
			if w.Code != 403 {
				t.Fatalf("group bypass: %s %s: %d %s", path, model, w.Code, w.Body.String())
			}
		}
		body := map[string]any{"model": "workbuddy/cn/shared", "max_tokens": 32, "messages": []map[string]string{{"role": "user", "content": "hi"}}}
		if path == "/v1/responses" || path == "/responses" {
			delete(body, "messages")
			body["input"] = "hi"
		}
		w := responseRequest(s, k.Key, path, string(mustJSON(body)))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "GROUP_OK") {
			t.Fatalf("allowed request failed %s %d %s", path, w.Code, w.Body.String())
		}
	}
	if calls != 4 {
		t.Fatal("blocked requests reached upstream", calls)
	}
	g.AccountIDs = []string{}
	if _, err = s.Store.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	if ids = modelIDsFromResponse(t, scopedModelRequest(s, k.Key)); len(ids) != 0 {
		t.Fatal("empty group exposed models", ids)
	}
	w := responseRequest(s, k.Key, "/v1/chat/completions", `{"model":"workbuddy/cn/shared","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 403 || calls != 4 {
		t.Fatal("empty group used another account")
	}
	if err = s.Store.SetKeyGroupAndSave(k.Key, ""); err != nil {
		t.Fatal(err)
	}
	if ids = modelIDsFromResponse(t, scopedModelRequest(s, k.Key)); len(ids) < 3 {
		t.Fatal("ungrouped catalog compatibility lost", ids)
	}
}

func TestGroupAdminBindingAndBoundDeletion(t *testing.T) {
	s := newAdminTestServer(t, "a")
	k := s.Store.AddKey("key")
	w := adminProbeRequest(t, s, "POST", "/admin/api/groups", `{"name":"group","account_ids":["a"],"allowlist_enabled":true,"model_ids":[]}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		Group store.AccountGroup `json:"group"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	w = adminProbeRequest(t, s, "POST", "/admin/api/keys/group", string(mustJSON(map[string]any{"ref": keyReference(k.Key), "group_id": result.Group.ID})))
	if w.Code != 200 || s.Store.Keys()[0].GroupID != result.Group.ID {
		t.Fatal("key group binding failed")
	}
	w = adminProbeRequest(t, s, "DELETE", "/admin/api/groups?id="+result.Group.ID, "")
	if w.Code != 409 {
		t.Fatal("bound group deleted")
	}
	w = adminProbeRequest(t, s, "POST", "/admin/api/keys", `{"name":"invalid","group_id":"missing"}`)
	if w.Code != 400 || len(s.Store.Keys()) != 1 {
		t.Fatal("invalid group key created")
	}
}

func TestGroupResolvedTargetsAndRevalidation(t *testing.T) {
	s := poolServer(t, 2)
	s.Reg.Replace([]registry.Model{{ID: "test-model"}, {ID: "blocked"}})
	s.cache.loadedAt = time.Now()
	s.Store.UpdateSettings(func(st *store.Settings) {
		st.ModelAliases["allowed-alias"] = "test-model"
		st.ModelAliases["blocked-alias"] = "blocked"
	})
	g, err := s.Store.SaveGroup(store.AccountGroup{Name: "restricted", AccountIDs: []string{"a"}, AllowlistEnabled: true, ModelIDs: []string{"test-model"}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddKeyInGroupAndSave("key", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := modelIDsFromResponse(t, scopedModelRequest(s, k.Key))
	if len(ids) != 2 {
		t.Fatal("allowed alias missing or blocked alias exposed", ids)
	}
	for _, id := range ids {
		if id != "test-model" && id != "allowed-alias" {
			t.Fatal("blocked model exposed", ids)
		}
	}
	r := httptest.NewRequest("GET", "/v1/client-profile?model=blocked", nil)
	r.Header.Set("Authorization", "Bearer "+k.Key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("profile bypass", w.Code)
	}
	w = responseRequest(s, k.Key, "/v1/chat/completions", `{"model":"unknown","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 404 || strings.Contains(w.Body.String(), "blocked") {
		t.Fatal("unknown model exposed other group catalog", w.Body.String())
	}
	s.Store.UpdateSettings(func(st *store.Settings) { st.ForceModel = "blocked" })
	w = responseRequest(s, k.Key, "/v1/chat/completions", `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 403 {
		t.Fatal("forced model bypass", w.Code)
	}
	s.Store.UpdateSettings(func(st *store.Settings) { st.ForceModel = "" })
	ctx := context.WithValue(context.Background(), groupKeyContext{}, k.Key)
	ctx = context.WithValue(ctx, routingModelKey{}, "test-model")
	calls := 0
	_, err = s.withAccountRetry(ctx, time.Second, func(_ context.Context, a *codearts.Account) error {
		calls++
		if a.ID != "a" {
			t.Fatal("outside group account selected", a.ID)
		}
		g.AccountIDs = nil
		if _, err := s.Store.SaveGroup(g); err != nil {
			t.Fatal(err)
		}
		return &provider.Error{Status: 503, Message: "fixture upstream unavailable"}
	})
	if calls != 1 || apierr.From(err).Status != 403 {
		t.Fatal("scope change ignored during retry", calls, err)
	}
}

func TestGroupDeniesForbiddenSummaryFallback(t *testing.T) {
	calls := 0
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return responseUpstream(r, summaryFixture("PRIVATE_PARTIAL", "length", 10, 4)), nil
	})
	s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}, {ID: "backup-model"}})
	p, _ := s.Store.ModelPolicy("deepseek-v4.1-flash")
	p.SummaryFallbackModel = "backup-model"
	if err := s.Store.SetModelPolicyAndSave("deepseek-v4.1-flash", p); err != nil {
		t.Fatal(err)
	}
	accountIDs := []string{}
	for _, a := range s.Store.Accounts() {
		accountIDs = append(accountIDs, a.ID)
	}
	g, err := s.Store.SaveGroup(store.AccountGroup{Name: "primary only", AccountIDs: accountIDs, AllowlistEnabled: true, ModelIDs: []string{"deepseek-v4.1-flash"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SetKeyGroupAndSave(key, g.ID); err != nil {
		t.Fatal(err)
	}
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":"You are a context summarization assistant."},{"role":"user","content":"history"}]}`)
	if w.Code != 403 || calls != 1 || strings.Contains(w.Body.String(), "PRIVATE_PARTIAL") {
		t.Fatal("summary fallback bypassed group or exposed partial output", calls, w.Code, w.Body.String())
	}
	if s.Store.Keys()[0].BudgetUsage.Tokens != 14 {
		t.Fatal("primary attempt usage lost")
	}
}
