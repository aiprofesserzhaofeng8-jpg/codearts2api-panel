package server

import (
	"codearts2api/internal/access"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConsumerWalletActualProtocolSettlementAndDeniedNoSpend(t *testing.T) {
	for _, platform := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
			for _, stream := range []bool{false, true} {
				t.Run(platform+path+fmt.Sprint(stream), func(t *testing.T) {
					s, _, g := mappedFixture(t, nil)
					onlyMappedPlatform(s, platform)
					u := createTestUser(t, s, "alice", g.ID)
					key, err := s.Store.AddUserKeyAndSave(u.ID, "metered")
					if err != nil {
						t.Fatal(err)
					}
					for _, model := range mappedSources {
						if err = s.Store.SetPriceAndSave(model, storage.ModelPrice{Currency: "USD", Input: 1, Output: 1}); err != nil {
							t.Fatal(err)
						}
					}
					if err = s.Store.AdjustWallet(u.ID, 0, 1000000000, true, false, "test grant"); err != nil {
						t.Fatal(err)
					}
					response := responseRequest(s, key.Key, path, mappedBody(path, stream, mappedPublic))
					if response.Code != 200 {
						t.Fatal(response.Code, response.Body.String())
					}
					got, _ := s.Store.User(u.ID)
					entries := s.Store.UserLedger(u.ID)
					if got.Wallet.Balance >= 1000000000 || got.Wallet.ReviewRequired || s.Store.UserReservedCredits(u.ID) != 0 || got.BudgetUsage.Requests != 1 || len(entries) != 2 {
						t.Fatal("actual protocol settlement failed", got.Wallet, entries)
					}
					if entries[0].Reference == "" || entries[0].Kind != "usage" {
						t.Fatal("missing settlement reference")
					}
					if err = s.Store.AdjustWallet(u.ID, got.Wallet.Revision, -got.Wallet.Balance, true, false, "drain credit"); err != nil {
						t.Fatal(err)
					}
					before := len(s.Store.UserLedger(u.ID))
					response = responseRequest(s, key.Key, path, mappedBody(path, stream, mappedPublic))
					if response.Code != 402 || len(s.Store.UserLedger(u.ID)) != before {
						t.Fatal("zero balance reached upstream or charged", response.Code)
					}
				})
			}
		}
	}
}
func TestConsumerHTTPUserIsolationAndAdminSecrets(t *testing.T) {
	s, _, g := mappedFixture(t, nil)
	alice := createTestUser(t, s, "alice", g.ID)
	bob := createTestUser(t, s, "bob", g.ID)
	cookie := loginTestUser(t, s, "alice")
	if err := s.Store.AdjustWallet(bob.ID, 0, 100, true, false, "bob only"); err != nil {
		t.Fatal(err)
	}
	response := userHTTP(s, cookie, "GET", "/user/api/entitlements?user_id="+bob.ID, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "bob only") {
		t.Fatal("other user financial history leaked", response.Body.String())
	}
	for _, path := range []string{"wallet", "plans", "subscription", "codes", "identity"} {
		if response = userHTTP(s, cookie, "GET", "/admin/api/consumer/"+path, ""); response.Code != 401 && response.Code != 403 {
			t.Fatal("user entered admin consumer route", path, response.Code)
		}
	}
	codes, err := s.Store.GenerateRedemptions(access.Redemption{Kind: "balance", Amount: 10000, ExpiresAt: time.Now().Add(time.Hour).Unix()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"code": codes[0], "user_id": bob.ID})
	if response = userHTTP(s, cookie, "POST", "/user/api/redeem", string(raw)); response.Code != 400 {
		t.Fatal("unknown ownership field accepted")
	}
	raw, _ = json.Marshal(map[string]string{"code": codes[0]})
	if response = userHTTP(s, cookie, "POST", "/user/api/redeem", string(raw)); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	got, _ := s.Store.User(alice.ID)
	if got.Wallet.Balance != 10000 {
		t.Fatal("wrong beneficiary")
	}
	request := httptestConsumerRequest("/user/api/redeem", string(raw), cookie)
	request.Header.Set("Origin", "https://attacker.example")
	w := newConsumerRecorder()
	s.Handler().ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("redemption CSRF accepted")
	}
	cfg := access.IdentitySettings{SMTPHost: "smtp.example", SMTPPort: 587, SMTPFrom: "from@example.com", SMTPPassword: "mail-secret", GitHub: access.OAuthSettings{ClientSecret: "oauth-secret"}}
	if err = s.Store.SaveIdentitySettings(cfg, false, false, false); err != nil {
		t.Fatal(err)
	}
	w = newConsumerRecorder()
	s.handleAdminIdentitySettings(w, httptestConsumerRequest("/admin/api/consumer/identity", "", nil))
	if strings.Contains(w.Body.String(), "mail-secret") || strings.Contains(w.Body.String(), "oauth-secret") {
		t.Fatal("admin read exposed secret")
	}
}

func httptestConsumerRequest(path, body string, cookie *http.Cookie) *http.Request {
	method := "POST"
	if body == "" {
		method = "GET"
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}
func newConsumerRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
