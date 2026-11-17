package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/cnapi"
	"codearts2api/internal/provider"
	"net/http"
	"strings"
	"testing"
)

func TestAccountProtocolSaveAndReadback(t *testing.T) {
	s := newAdminTestServer(t, "native")
	c := cnapi.New("kimi", "Kimi", "https://fixture.invalid/v1", "", nil)
	s.Providers = provider.NewRegistry()
	_ = s.Providers.Register(c)
	raw, id, err := c.Normalize([]byte(`{"api_key":"fixture-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.UpsertAccountAndSave(&accounts.Account{ID: "kimi", Provider: "kimi", Scope: "cn", ExternalID: id.ID, Credentials: raw, Enabled: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	w := adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"protocol","id":"kimi","api_protocol":"adaptive"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got, _ := c.ProtocolOptions(s.Store.Account("kimi").Credentials); got != provider.AdaptiveProtocol {
		t.Fatal(got)
	}
	w = adminProbeRequest(t, s, http.MethodGet, "/admin/api/state", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"api_protocol":"adaptive"`) || strings.Contains(w.Body.String(), "fixture-secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"protocol","id":"kimi","api_protocol":"invalid"}`)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	if got, _ := c.ProtocolOptions(s.Store.Account("kimi").Credentials); got != provider.AdaptiveProtocol {
		t.Fatal("invalid save changed account")
	}
}
