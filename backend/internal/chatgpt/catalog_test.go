package chatgpt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelCatalogNegotiation(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/codex/models":
			version := r.URL.Query().Get("client_version")
			if version != codexCatalogVersion || version == "0.144.0" || r.Header.Get("Version") != version ||
				r.UserAgent() != "codex_cli_rs/"+version || r.Header.Get("Originator") != "codex_cli_rs" ||
				r.Header.Get("Accept") != "application/json" || r.Header.Get("ChatGPT-Account-Id") != "test-account" {
				t.Error("inconsistent Codex manifest request")
			}
			w.Write([]byte(`{"models":[{"slug":"a"},{"slug":"b"},{"slug":"c"},{"slug":"d"},{"slug":"e"},{"slug":"gpt-6.2-astra"}]}`))
		case "/api/models":
			if r.URL.RawQuery != "" || r.Header.Get("Version") != "" || r.Header.Get("Originator") != "" {
				t.Error("Codex identity leaked into API-key catalog request")
			}
			w.Write([]byte(`{"data":[{"id":"api-model"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer up.Close()
	c := New()
	c.API, c.Backend = up.URL+"/api", up.URL+"/codex"
	for _, tc := range []struct {
		raw, last string
		count     int
	}{
		{`{"auth_mode":"codex","access_token":"fixture","account_id":"test-account"}`, "gpt-6.2-astra", 6},
		{`{"api_key":"sk-fixture"}`, "api-model", 1},
	} {
		rows, err := c.Models(context.Background(), json.RawMessage(tc.raw))
		if err != nil || len(rows) != tc.count || rows[len(rows)-1].ID != tc.last {
			t.Fatalf("catalog: %v, %v", rows, err)
		}
	}
}
