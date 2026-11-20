package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/upstream/vertex"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type vertexRoundTrip func(*http.Request) (*http.Response, error)

func (f vertexRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func vertexFixtureStream() string {
	return "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"VERTEX_OK\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":3}}\n\n"
}
func vertexServerCredential(t *testing.T) json.RawMessage {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(map[string]any{"service_account": map[string]string{"type": "service_account", "project_id": "test-project", "client_email": "test@test-project.iam.gserviceaccount.com", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}, "location": "global", "access_token": "fixture-token", "expires_at": time.Now().Add(time.Hour).Unix(), "model_ids": []string{"gemini-fixture"}})
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestVertexProtocolsSharedMappingAndPermissions(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(path+map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
				s := newAdminTestServer(t)
				c := vertex.New()
				calls := 0
				c.HTTP.Transport = vertexRoundTrip(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.Host != "aiplatform.googleapis.com" {
						t.Error("bad authenticated destination")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(vertexFixtureStream()))}, nil
				})
				s.Providers = provider.NewRegistry()
				s.Providers.Register(c)
				raw := vertexServerCredential(t)
				_, id, err := c.Normalize(raw)
				if err != nil {
					t.Fatal(err)
				}
				s.Store.UpsertAccount(&accounts.Account{ID: "gcp", Provider: provider.Vertex, Scope: id.Scope, ExternalID: id.ID, Credentials: raw, Enabled: true})
				model := "vertex/global/gemini-fixture"
				s.Reg.ReplaceProvider(provider.Vertex, id.Scope, []provider.Model{{ID: "gemini-fixture", Name: "fixture"}})
				if _, err = s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "vertex-fixture-v1", Enabled: true, Sources: []config.ModelSource{{ID: model, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil); err != nil {
					t.Fatal(err)
				}
				key := s.Store.AddKey("fixture").Key
				_ = s.Handler()
				markModelsFresh(s)
				w := responseRequest(s, key, path, mappedBody(path, stream, "vertex-fixture-v1"))
				if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "VERTEX_OK") {
					t.Fatal(w.Code, w.Body.String(), calls)
				}
				logs := s.Store.Logs(1)
				if len(logs) != 1 || logs[0].Model != model || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 3 || s.Store.PoolSnapshot()["gcp"].InFlight != 0 {
					t.Fatal(logs)
				}
				g, err := s.Store.SaveGroup(storage.AccountGroup{Name: "empty", AccountIDs: []string{}})
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Store.SetKeyGroupAndSave(key, g.ID); err != nil {
					t.Fatal(err)
				}
				w = responseRequest(s, key, path, mappedBody(path, stream, "vertex-fixture-v1"))
				if w.Code != 403 || calls != 1 {
					t.Fatal("permission bypass", w.Code, calls)
				}
			})
		}
	}
}
