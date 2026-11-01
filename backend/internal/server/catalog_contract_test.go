package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// The identical test is compiled in a private frozen baseline checkout. It
// exercises actual HTTP guards, native adapters, credentials, refresh and probe
// coordination, while every upstream request is answered by a fake transport.
func TestAcceptanceCatalogContract(t *testing.T) {
	var records []map[string]any
	var mu sync.Mutex
	var calls []string
	note := func(s string) { mu.Lock(); calls = append(calls, s); mu.Unlock() }
	record := func(label string, w *httptest.ResponseRecorder) {
		var response any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if obj, ok := response.(map[string]any); ok {
			if data, ok := obj["data"].([]any); ok {
				for _, item := range data {
					item.(map[string]any)["created"] = 0
				}
			}
		}
		mu.Lock()
		sort.Strings(calls)
		requestCalls := append([]string{}, calls...)
		calls = nil
		mu.Unlock()
		records = append(records, map[string]any{"case": label, "status": w.Code, "response": response, "upstream_calls": requestCalls})
	}
	state := func(label string, s *Server) {
		w := adminProbeRequest(t, s, "GET", "/admin/api/state", "")
		var obj map[string]any
		if json.Unmarshal(w.Body.Bytes(), &obj) != nil {
			t.Fatal("invalid state")
		}
		records = append(records, map[string]any{"case": label, "models": obj["models"], "provider_errors": obj["provider_errors"], "upstream_error": obj["upstream_error"]})
	}
	s := newAdminTestServer(t, "a", "b")
	mode := "success"
	s.Up.HTTP = &http.Client{Transport: probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			note("native/catalog")
			if mode == "failure" {
				return probeFakeResponse(r, 503, "catalog unavailable"), nil
			}
			if mode == "empty" {
				return probeFakeResponse(r, 200, `{"builtinModels":[]}`), nil
			}
			return probeFakeResponse(r, 200, `{"builtinModels":[{"model_id":"fresh","model_name":"Fresh","enable":true,"credit":[{"ratio_display":"0.18x"}]},{"model_id":"disabled","enable":false}]}`), nil
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid native body")
		}
		account := "b"
		if strings.Contains(r.Header.Get("Authorization"), "Access=aka") {
			account = "a"
		}
		note(fmt.Sprintf("native/%s/%s/%d", account, body.Model, body.MaxTokens))
		if body.Model == "bad" || body.Model == "fallback" && account == "a" {
			return probeFakeResponse(r, 200, `{"error_code":"4005","error_msg":"unsupported model"}`), nil
		}
		return probeFakeResponse(r, 200, `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`), nil
	})}
	refresh := "/admin/api/models/refresh"
	probe := "/admin/api/models/probe"
	record("refresh-wrong-method", adminProbeRequest(t, s, "GET", refresh, ""))
	record("refresh-native", adminProbeRequest(t, s, "POST", refresh, "{}"))
	state("refreshed-state", s)
	record("warm-visible-models", adminProbeRequest(t, s, "GET", "/v1/models", ""))
	mode = "failure"
	record("refresh-failed", adminProbeRequest(t, s, "POST", refresh, "{}"))
	state("failed-state", s)
	record("failed-ttl-suppresses-refetch", adminProbeRequest(t, s, "GET", "/v1/models", ""))
	mode = "empty"
	record("refresh-empty-native", adminProbeRequest(t, s, "POST", refresh, "{}"))
	state("empty-native-state", s)
	mode = "success"
	record("refresh-recovered", adminProbeRequest(t, s, "POST", refresh, "{}"))
	state("recovered-state", s)
	s.Store.UpdateSettings(func(st *store.Settings) { st.ModelAliases["fresh-alias"] = "fresh" })
	for _, fixture := range []struct{ label, method, body string }{
		{"probe-dedupe-order", "POST", `{"models":[" good ","bad","good","fallback"," "]}`},
		{"probe-selected-account", "POST", `{"id":"b","models":["fallback"]}`},
		{"probe-missing-account", "POST", `{"id":"missing","models":["good"]}`},
		{"probe-empty-list", "POST", `{"models":[]}`},
		{"probe-null", "POST", `null`},
		{"probe-malformed-keeps-legacy-default", "POST", `{`},
		{"probe-array-keeps-legacy-default", "POST", `[]`},
		{"probe-get-keeps-legacy-default", "GET", `{"models":["good"]}`},
		{"probe-type-error-keeps-decoded-id", "POST", `{"id":"missing","models":true}`},
		{"probe-empty-values", "POST", `{"models":[" ",""]}`},
		{"probe-cross-provider", "POST", `{"models":["trae/cn/missing"]}`},
	} {
		record(fixture.label, adminProbeRequest(t, s, fixture.method, probe, fixture.body))
	}
	many := make([]string, 65)
	for i := range many {
		many[i] = fmt.Sprintf("m%d", i)
	}
	encoded, _ := json.Marshal(map[string]any{"models": many})
	record("probe-limit", adminProbeRequest(t, s, "POST", probe, string(encoded)))
	s.Store.SetAccountEnabled("b", false)
	record("probe-explicit-disabled-retains-legacy-behavior", adminProbeRequest(t, s, "POST", probe, `{"id":"b","models":["good"]}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", probe, strings.NewReader(`{"models":["good"]}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	s.handleAdminModelProbe(w, r)
	record("probe-canceled", w)

	providerMode := "success"
	wb, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/config" {
			scope := "cn"
			if strings.Contains(r.Header.Get("Authorization"), "global") {
				scope = "global"
			}
			note("workbuddy/" + scope + "/catalog")
			if providerMode == "failure" {
				w.WriteHeader(503)
				io.WriteString(w, "catalog unavailable")
				return
			}
			if providerMode == "empty" {
				io.WriteString(w, `{"code":0,"data":{"models":[]}}`)
				return
			}
			io.WriteString(w, `{"code":0,"data":{"models":[{"id":"fresh","name":"Fresh","maxInputTokens":64000,"maxOutputTokens":4096}]}}`)
			return
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		note(fmt.Sprintf("workbuddy/probe/%s/%d", body.Model, body.MaxTokens))
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, summaryFixture("OK", "stop", 10, 4))
	})
	wb.AllowLocalAdmin = true
	wb.Store.UpdateSettings(func(st *store.Settings) { st.AdminPassword = "" })
	addWB(t, wb, "cn", time.Now().Add(time.Hour).Unix())
	addWB(t, wb, "global", time.Now().Add(time.Hour).Unix())
	wb.Reg.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "retained"}})
	record("refresh-provider-only", adminProbeRequest(t, wb, "POST", refresh, "{}"))
	state("provider-only-state", wb)
	record("probe-provider-scope", adminProbeRequest(t, wb, "POST", probe, `{"models":["workbuddy/cn/fresh","workbuddy/global/fresh"]}`))
	record("probe-provider-unknown", adminProbeRequest(t, wb, "POST", probe, `{"models":["workbuddy/cn/missing"]}`))
	record("probe-selected-wrong-scope", adminProbeRequest(t, wb, "POST", probe, `{"id":"wb-cn","models":["workbuddy/global/fresh"]}`))
	providerMode = "failure"
	record("refresh-provider-failed", adminProbeRequest(t, wb, "POST", refresh, "{}"))
	state("provider-failed-state", wb)
	record("provider-failure-warm-models", adminProbeRequest(t, wb, "GET", "/v1/models", ""))
	providerMode = "empty"
	record("refresh-provider-empty", adminProbeRequest(t, wb, "POST", refresh, "{}"))
	state("provider-empty-state", wb)
	none := newAdminTestServer(t)
	record("refresh-no-accounts", adminProbeRequest(t, none, "POST", refresh, "{}"))
	record("probe-no-accounts", adminProbeRequest(t, none, "POST", probe, "{}"))
	empty := newAdminTestServer(t, "empty")
	empty.Reg.Replace([]registry.Model{})
	record("probe-empty-catalog", adminProbeRequest(t, empty, "POST", probe, "{}"))
	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("CATALOG_CONTRACT " + string(raw))
}

func BenchmarkCatalogWarmModels(b *testing.B) {
	st, err := store.Open(filepath.Join(b.TempDir(), "state.json"))
	if err != nil {
		b.Fatal(err)
	}
	st.UpdateSettings(func(s *store.Settings) { s.RateLimitEnabled = false })
	st.UpsertAccount(&accounts.Account{ID: "fixture", Enabled: true, Session: &codearts.Session{
		AccessKey: "fixture-ak", SecretKey: "fixture-sk", CredentialKind: codearts.CredentialKindAKSK,
	}})
	s := newConfiguredServer(st)
	s.Up.HTTP = &http.Client{Transport: probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return probeFakeResponse(r, 200, `{"builtinModels":[{"model_id":"fresh","model_name":"Fresh","enable":true}]}`), nil
	})}
	if err := s.RefreshModels(context.Background()); err != nil {
		b.Fatal(err)
	}
	key, handler := st.AddKey("fixture").Key, s.Handler()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r := httptest.NewRequest("GET", "/v1/models", nil)
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}
