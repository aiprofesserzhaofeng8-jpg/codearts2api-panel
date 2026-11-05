package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/app"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

// This adapter is confined to tests: all catalog, credential and probe calls
// stay in-process, while the browser uses real authenticated application APIs.
type unifiedUIAdapter struct{ platform string }

func (a unifiedUIAdapter) Describe() provider.Descriptor {
	return provider.Descriptor{ID: a.platform, Name: a.platform, Available: true, Scopes: []string{"cn"}}
}
func (a unifiedUIAdapter) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	return raw, provider.Identity{ID: "synthetic-ui-user", Name: a.platform, Scope: "cn", NonExpiring: true}, nil
}
func (a unifiedUIAdapter) Refresh(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return raw, nil
}
func (a unifiedUIAdapter) Stream(_ context.Context, _ json.RawMessage, _ provider.Request, emit func([]byte) error) error {
	return emit([]byte(`{"id":"synthetic","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`))
}
func (a unifiedUIAdapter) Models(context.Context, json.RawMessage) ([]provider.Model, error) {
	return []provider.Model{{ID: "shared-v1", Name: "Synthetic shared model", Context: 65536, MaxOutput: 8192}}, nil
}
func (a unifiedUIAdapter) Credit(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func TestUnifiedModelsBrowser(t *testing.T) {
	if os.Getenv("CODEARTS_U5_BROWSER") != "1" {
		t.Skip("opt-in isolated browser acceptance")
	}
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	password := "synthetic-u5-admin-password"
	if err := st.UpdateSettings(func(s *config.Settings) {
		s.AdminPassword = password
		s.AutoRefresh = false
		s.AutoClaim = false
		s.RateLimitEnabled = false
	}); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{provider.WorkBuddy, provider.Trae} {
		st.UpsertAccount(&accounts.Account{ID: "synthetic-" + platform, Provider: platform, Scope: "cn", ExternalID: "synthetic-ui-user", Name: platform + " fixture", Enabled: true, Credentials: json.RawMessage(`{"fixture":true}`)})
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	srv, err := app.Build(st)
	if err != nil {
		t.Fatal(err)
	}
	srv.Providers = provider.NewRegistry()
	for _, platform := range []string{provider.WorkBuddy, provider.Trae} {
		adapter := unifiedUIAdapter{platform}
		if err := srv.Providers.Register(adapter); err != nil {
			t.Fatal(err)
		}
		models, _ := adapter.Models(context.Background(), nil)
		srv.Reg.ReplaceProvider(platform, "cn", models)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/fixture/remove-source", func(w http.ResponseWriter, r *http.Request) {
		srv.Reg.ReplaceProvider(provider.Trae, "cn", nil)
		w.WriteHeader(204)
	})
	mux.Handle("/", srv.Handler())
	testServer := httptest.NewServer(mux)
	defer testServer.Close()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", filepath.Join(root, "scripts/unified-models-smoke.cjs"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CODEARTS_TEST_BASE="+testServer.URL, "CODEARTS_TEST_ADMIN_PASSWORD="+password, "NODE_PATH="+filepath.Join(root, "frontend/node_modules"))
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reopened.UnifiedSnapshot()
	if len(snapshot.Models) != 1 || len(snapshot.Models[0].Sources) != 2 || snapshot.Models[0].Sources[1].Enabled {
		t.Fatal("browser edits not durably retained", snapshot.Models)
	}
	groups := reopened.CanonicalGroups()
	if len(groups) != 1 || len(groups[0].UnifiedModels) != 1 || groups[0].ClientDefaultModel != snapshot.Models[0].Name {
		t.Fatal("browser group authorization not durable", groups)
	}
}
