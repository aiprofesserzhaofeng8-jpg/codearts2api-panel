package server

import (
	"codearts2api/internal/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromptManagementAndPreview(t *testing.T) {
	s := newAdminTestServer(t, "fixture")
	before := s.Store.Settings().PromptConfiguration()
	next := s.Store.Settings().PromptConfiguration()
	next.Common.Mode = "append"
	next.Common.Text = "EXTRA"
	raw, _ := json.Marshal(map[string]config.PromptConfiguration{"expected": before, "configuration": next})
	w := adminProbeRequest(t, s, http.MethodPost, "/admin/api/prompts", string(raw))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = adminProbeRequest(t, s, http.MethodPost, "/admin/api/prompts", string(raw))
	if w.Code != 409 {
		t.Fatal("stale write accepted", w.Code)
	}
	w = adminProbeRequest(t, s, http.MethodPost, "/admin/api/prompts/preview", `{"protocol":"responses","mode":"append","text":"PREVIEW ONLY","request":{"instructions":"CLIENT","input":"test"}}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "CLIENT\\n\\nPREVIEW ONLY") {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.Store.Settings().CommonPrompt.Text != "EXTRA" {
		t.Fatal("preview wrote config")
	}
	w = adminProbeRequest(t, s, http.MethodPost, "/admin/api/prompts/preview", `{"protocol":"invalid","mode":"append","text":"x","request":{}}`)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestPromptEndpointsRequireAdministrator(t *testing.T) {
	s := newAdminTestServer(t)
	for _, path := range []string{"/admin/api/prompts", "/admin/api/prompts/preview"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Host = "127.0.0.1:10087"
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("unauthenticated request accepted: %s %d", path, w.Code)
		}
	}
}
