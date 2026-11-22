package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"codearts2api/internal/storage"
)

func TestV1AuthRejectsMissingKey(t *testing.T) {
	s := newTestServer(t, 0, 1)
	h := s.withAuth(okHandler())

	cases := []struct {
		name    string
		addr    string
		headers map[string]string
		want    int
	}{
		{"本机直连", "127.0.0.1:5555", nil, http.StatusUnauthorized},
		{"远端直连", "203.0.113.7:5555", nil, http.StatusUnauthorized},
		{"代理转发", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "203.0.113.7"}, http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h(rec, adminRequest(c.addr, c.headers))
			if rec.Code != c.want {
				t.Errorf("状态码 = %d，期望 %d", rec.Code, c.want)
			}
		})
	}
}

func TestV1AuthIgnoresHistoricAnonymousSetting(t *testing.T) {
	s := newTestServer(t, 0, 1)
	s.Store.UpdateSettings(func(st *storage.Settings) { st.AllowAnonymous = true })
	h := s.withAuth(okHandler())

	rec := httptest.NewRecorder()
	h(rec, adminRequest("203.0.113.7:5555", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("开启 allow_anonymous 后状态码 = %d，期望 401", rec.Code)
	}
}

// TestV1AuthRequiresValidKeyOnceIssued 一旦签发了密钥，任何来源都必须携带有效密钥。
func TestV1AuthRequiresValidKeyOnceIssued(t *testing.T) {
	s := newTestServer(t, 0, 1, "test")
	key := s.Store.Keys()[0]
	h := s.withAuth(okHandler())

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"无密钥", nil, http.StatusUnauthorized},
		{"错误密钥", map[string]string{"Authorization": "Bearer sk-ca-nope"}, http.StatusUnauthorized},
		{"正确密钥", map[string]string{"Authorization": "Bearer " + key.Key}, http.StatusOK},
		{"x-api-key 形式", map[string]string{"x-api-key": key.Key}, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h(rec, adminRequest("203.0.113.7:5555", c.headers))
			if rec.Code != c.want {
				t.Errorf("状态码 = %d，期望 %d", rec.Code, c.want)
			}
		})
	}

	// 停用后立即失效
	s.Store.ToggleKey(key.Key)
	rec := httptest.NewRecorder()
	h(rec, adminRequest("203.0.113.7:5555", map[string]string{"Authorization": "Bearer " + key.Key}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("停用密钥后状态码 = %d，期望 401", rec.Code)
	}
}

// A persisted ungrouped key must never reach routing, including after a restart.
func TestHistoricalUngroupedKeyRequiresExplicitAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	const key = "sk-fixture-historical-unassigned"
	data, err := json.Marshal(map[string]any{"version": storage.StateVersion, "model_routing_revision": 1, "unified_models": []any{}, "keys": []storage.APIKey{{Name: "legacy", Key: key, Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	st, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := newConfiguredServer(st)
	checkDenied := func() {
		t.Helper()
		for _, route := range []string{"/v1/models", "/v1/client-profile", "/v1/chat/completions", "/v1/responses", "/responses", "/v1/messages", "/v1/search/messages", "/v1/systemone"} {
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, v1Request(route, key))
			if rec.Code != 403 {
				t.Fatalf("%s: status %d", route, rec.Code)
			}
		}
		checkModelError(t, adminProbeRequest(t, s, "GET", "/admin/api/keys/models?ref="+keyReference(key), ""), 403, "group_unavailable")
	}
	checkDenied()
	a, err := st.SaveGroup(storage.AccountGroup{Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetKeyGroupAndSave(key, a.ID); err != nil {
		t.Fatal(err)
	}
	if w := scopedModelRequest(s, key); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if err = st.DeleteGroup(a.ID); err != nil {
		t.Fatal(err)
	}
	st, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s = newConfiguredServer(st)
	checkDenied()
	if got := st.Keys(); len(got) != 1 || got[0].GroupID != a.ID {
		t.Fatal("deleted group reference lost")
	}
	b, err := st.SaveGroup(storage.AccountGroup{Name: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetKeyGroupAndSave(key, b.ID); err != nil {
		t.Fatal(err)
	}
	if w := scopedModelRequest(s, key); w.Code != 200 {
		t.Fatal(w.Code)
	}
}
