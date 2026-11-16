package server

import (
	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/typesafe"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const decisionBody = `{"model":"jev-latest","state":"test","questions":{"q":{"type":"noul"}}}`
const decisionModel = "typesafe/global/jev-latest"

func decisionConsumerFixture(t *testing.T, usage string) (*Server, access.UserView, string, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"answers":{"q":{"type":"noul","noul":0.8}},"usage":`+usage+`}`)
	}))
	t.Cleanup(up.Close)
	s := newAdminTestServer(t)
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
	g, err := s.Store.SaveGroup(storage.AccountGroup{Name: "native", AccountIDs: []string{"decision"}, AllowlistEnabled: true, ModelIDs: []string{decisionModel}})
	if err != nil {
		t.Fatal(err)
	}
	u := createTestUser(t, s, "native-user", g.ID)
	k, err := s.Store.AddUserKeyAndSave(u.ID, "native")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetPriceAndSave(decisionModel, storage.ModelPrice{Currency: "USD", Input: 1, Output: 1}); err != nil {
		t.Fatal(err)
	}
	_ = s.Handler()
	return s, u, k.Key, calls
}

func TestSystemOneConsumerWallet(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "metered", false: "unknown"}[known], func(t *testing.T) {
			usage := `null`
			if known {
				usage = `{"input_tokens":12,"output_tokens":3}`
			}
			s, u, key, calls := decisionConsumerFixture(t, usage)
			if err := s.Store.AdjustWallet(u.ID, 0, 1000000000, true, false, "fixture"); err != nil {
				t.Fatal(err)
			}
			w := responseRequest(s, key, "/v1/systemone", decisionBody)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			got, _ := s.Store.User(u.ID)
			if got.Wallet.ReviewRequired == known || s.Store.UserReservedCredits(u.ID) != 0 || s.Store.Users()[0].InFlight != 0 || got.BudgetUsage.Requests != 1 {
				t.Fatal(got)
			}
			if known {
				if got.Wallet.Balance >= 1000000000 {
					t.Fatal("usage not charged")
				}
				if err := s.Store.AdjustWallet(u.ID, got.Wallet.Revision, -got.Wallet.Balance, true, false, "drain"); err != nil {
					t.Fatal(err)
				}
			} else if got.Wallet.Balance != 1000000000 {
				t.Fatal("unknown usage charged as known")
			}
			w = responseRequest(s, key, "/v1/systemone", decisionBody)
			if w.Code != 402 || calls.Load() != 1 || s.Store.UserReservedCredits(u.ID) != 0 {
				t.Fatal(w.Code, w.Body.String(), calls.Load())
			}
		})
	}
}

func TestSystemOneConsumerBudgetAndConcurrency(t *testing.T) {
	s, u, key, calls := decisionConsumerFixture(t, `{"input_tokens":12,"output_tokens":3}`)
	limit := 1
	if _, err := s.Store.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, MaxConcurrency: &limit, Budget: &access.KeyBudget{MaxRequests: 1}}, ""); err != nil {
		t.Fatal(err)
	}
	second, err := s.Store.AddUserKeyAndSave(u.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.Store.BeginKeyBudget(key)
	if err != nil {
		t.Fatal(err)
	}
	w := responseRequest(s, second.Key, "/v1/systemone", decisionBody)
	release()
	if w.Code != 429 || calls.Load() != 0 {
		t.Fatal("concurrency bypass", w.Code, calls.Load())
	}
	w = responseRequest(s, second.Key, "/v1/systemone", decisionBody)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = responseRequest(s, key, "/v1/systemone", decisionBody)
	if w.Code != 402 || calls.Load() != 1 {
		t.Fatal("shared budget bypass", w.Code, w.Body.String(), calls.Load())
	}
	if s.Store.Users()[0].InFlight != 0 || s.Store.PoolSnapshot()["decision"].InFlight != 0 {
		t.Fatal("lease leak")
	}
}

func TestSystemOneConsumerModelAllowlist(t *testing.T) {
	s, u, key, calls := decisionConsumerFixture(t, `{"input_tokens":12,"output_tokens":3}`)
	body := `{"model":"jev-other","state":"test","questions":{"q":{"type":"noul"}}}`
	w := responseRequest(s, key, "/v1/systemone", body)
	if w.Code != 403 || calls.Load() != 0 {
		t.Fatal("allowlist bypass", w.Code, w.Body.String())
	}
	w = responseRequest(s, key, "/v1/systemone", decisionBody)
	if w.Code != 200 || calls.Load() != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.Store.Users()[0].InFlight != 0 || s.Store.UserReservedCredits(u.ID) != 0 {
		t.Fatal("lease leak")
	}
}

func TestSystemOneConsumerUnpricedRequestDoesNotReachUpstream(t *testing.T) {
	s, u, key, calls := decisionConsumerFixture(t, `null`)
	if err := s.Store.SetPriceAndSave(decisionModel, storage.ModelPrice{Currency: "USD", Input: -1, Output: -1}); err == nil {
		t.Fatal("invalid price accepted")
	}
	// An allowed native model without a price must be denied before generation.
	g, err := s.Store.SaveGroup(storage.AccountGroup{ID: u.GroupID, Name: "native", AccountIDs: []string{"decision"}, AllowlistEnabled: true, ModelIDs: []string{"typesafe/global/jev-unpriced"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = g
	if err := s.Store.AdjustWallet(u.ID, 0, 1000000000, true, false, "fixture"); err != nil {
		t.Fatal(err)
	}
	w := responseRequest(s, key, "/v1/systemone", `{"model":"jev-unpriced","state":"x","questions":{"q":{"type":"noul"}}}`)
	if w.Code != 402 || calls.Load() != 0 || s.Store.UserReservedCredits(u.ID) != 0 || s.Store.Users()[0].InFlight != 0 {
		t.Fatal(w.Code, w.Body.String(), calls.Load())
	}
}
