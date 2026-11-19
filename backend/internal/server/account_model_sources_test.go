package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/typesafe"
	"codearts2api/internal/vertex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccountSyncNonChatSources(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != "GET" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"models":[{"name":"jev-fixture"}]}`)
	}))
	defer up.Close()
	ts := typesafe.New()
	ts.Origin = up.URL
	for _, tc := range []struct {
		p        provider.Adapter
		raw      json.RawMessage
		kind, id string
	}{
		{ts, json.RawMessage(`{"api_key":"fixture"}`), "decision", "jev-fixture"},
		{vertex.New(), vertexServerCredential(t), "configured", "gemini-fixture"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s := newAdminTestServer(t)
			s.Providers = provider.NewRegistry()
			if err := s.Providers.Register(tc.p); err != nil {
				t.Fatal(err)
			}
			raw, id, err := tc.p.Normalize(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			s.Store.UpsertAccount(&accounts.Account{ID: "a", Provider: tc.p.Describe().ID, Scope: id.Scope, ExternalID: id.ID, Credentials: raw, Enabled: true})
			w := adminProbeRequest(t, s, "POST", "/admin/api/accounts/models?id=a", "")
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			_, snap := s.Store.ModelSyncTarget("a")
			if snap == nil || snap.List.Kind != tc.kind || len(snap.List.Models) == 0 || snap.List.Models[0].ID != tc.id || len(snap.Published) > 0 {
				t.Fatal(snap)
			}
		})
	}
}
