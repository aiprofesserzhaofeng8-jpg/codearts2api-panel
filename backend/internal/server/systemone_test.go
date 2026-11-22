package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/typesafe"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSystemOneNativeAccountingAndPermissions(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "known", false: "unknown"}[known], func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("wrong transport")
				}
				b, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(b), `"model":"jev-latest"`) {
					t.Error(string(b))
				}
				usage := `null`
				if known {
					usage = `{"input_tokens":12,"output_tokens":3}`
				}
				io.WriteString(w, `{"model":"jev-1.14.0","answers":{"q":{"type":"noul","noul":0.8}},"usage":`+usage+`}`)
			}))
			defer up.Close()
			s := adminTestServerWithKeys(t, []string{"fixture"})
			c := typesafe.New()
			c.Origin = up.URL
			s.Providers = provider.NewRegistry()
			s.Providers.Register(c)
			raw := json.RawMessage(`{"api_key":"fixture"}`)
			_, id, err := c.Normalize(raw)
			if err != nil {
				t.Fatal(err)
			}
			s.Store.UpsertAccount(&accounts.Account{ID: "decision", Provider: provider.TypeSafe, Scope: "global", ExternalID: id.ID, Credentials: raw, Enabled: true})
			grantFixtureAccounts(t, s.Store)
			key := s.Store.Keys()[0].Key
			_ = s.Handler()
			group, err := s.Store.SaveGroup(storage.AccountGroup{Name: "decision allowed", AccountIDs: []string{"decision"}})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Store.SetKeyGroupAndSave(key, group.ID); err != nil {
				t.Fatal(err)
			}
			body := `{"model":"jev-latest","state":"test","questions":{"q":{"type":"noul"}}}`
			w := responseRequest(s, key, "/v1/systemone", body)
			if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), `"noul":0.8`) {
				t.Fatal(w.Code, w.Body.String(), calls)
			}
			logs := s.Store.Logs(1)
			if len(logs) != 1 || logs[0].Usage.Known != known || known && (logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 3) || s.Store.PoolSnapshot()["decision"].InFlight != 0 {
				t.Fatal(logs)
			}
			denied, err := s.Store.SaveGroup(storage.AccountGroup{Name: "empty", AccountIDs: []string{}})
			if err != nil {
				t.Fatal(err)
			}
			s.Store.SetKeyGroupAndSave(key, denied.ID)
			w = responseRequest(s, key, "/v1/systemone", body)
			if w.Code != 403 || calls != 1 {
				t.Fatal("group bypass", w.Code, w.Body.String(), calls)
			}
			w = responseRequest(s, "wrong", "/v1/systemone", body)
			if w.Code != 401 || calls != 1 {
				t.Fatal(w.Code, calls)
			}
		})
	}
}

func TestSystemOneErrorsReleaseLeaseAndNeverRetry(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"secret"}`)
			}))
			defer up.Close()
			s := adminTestServerWithKeys(t, []string{"test"})
			c := typesafe.New()
			c.Origin = up.URL
			s.Providers = provider.NewRegistry()
			s.Providers.Register(c)
			raw := json.RawMessage(`{"api_key":"fixture"}`)
			_, id, _ := c.Normalize(raw)
			s.Store.UpsertAccount(&accounts.Account{ID: "decision", Provider: provider.TypeSafe, Scope: "global", ExternalID: id.ID, Credentials: raw, Enabled: true})
			grantFixtureAccounts(t, s.Store)
			key := s.Store.Keys()[0].Key
			w := responseRequest(s, key, "/v1/systemone", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul"}}}`)
			expected := 502
			if status == 429 {
				expected = 429
			}
			if w.Code != expected || calls != 1 || strings.Contains(w.Body.String(), "secret") || s.Store.PoolSnapshot()["decision"].InFlight != 0 {
				t.Fatal(w.Code, w.Body.String(), calls)
			}
			w = responseRequest(s, key, "/v1/systemone", `{"model":"jev-latest","model":"other","state":"x","questions":{"q":{"type":"noul"}}}`)
			if w.Code != 400 || calls != 1 {
				t.Fatal("invalid request sent", w.Code, calls)
			}
			models, _ := c.Models(t.Context(), raw)
			if len(models) != 0 {
				t.Fatal("decisions leaked into chat catalog")
			}
		})
	}
}

func TestSystemOneCancellationReleasesAccount(t *testing.T) {
	entered := make(chan struct{})
	cleanup := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-cleanup:
		}
	}))
	defer up.Close()
	defer close(cleanup)
	s := newAdminTestServer(t)
	c := typesafe.New()
	c.Origin = up.URL
	s.Providers = provider.NewRegistry()
	s.Providers.Register(c)
	raw := json.RawMessage(`{"api_key":"fixture"}`)
	_, id, _ := c.Normalize(raw)
	s.Store.UpsertAccount(&accounts.Account{ID: "decision", Provider: provider.TypeSafe, Scope: "global", ExternalID: id.ID, Credentials: raw, Enabled: true})
	group, err := s.Store.SaveGroup(storage.AccountGroup{Name: "cancel-native", AccountIDs: []string{"decision"}})
	if err != nil {
		t.Fatal(err)
	}
	user := createTestUser(t, s, "cancel-native", group.ID)
	userKey, err := s.Store.AddUserKeyAndSave(user.ID, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	key := userKey.Key
	if err := s.Store.SetPriceAndSave(decisionModel, storage.ModelPrice{Currency: "USD", Input: 1, Output: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.AdjustWallet(user.ID, 0, 1000000000, true, false, "fixture"); err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(`{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul"}}}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+key)
	done := make(chan struct{})
	go func() { defer close(done); handler.ServeHTTP(httptest.NewRecorder(), req) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not called")
	}
	if s.Store.Users()[0].InFlight != 1 || s.Store.UserReservedCredits(user.ID) <= 0 {
		t.Error("consumer reservation missing before cancellation")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation stuck")
	}
	if s.Store.PoolSnapshot()["decision"].InFlight != 0 || s.Store.Users()[0].InFlight != 0 || s.Store.UserReservedCredits(user.ID) != 0 {
		t.Fatal("lease leaked")
	}
}
