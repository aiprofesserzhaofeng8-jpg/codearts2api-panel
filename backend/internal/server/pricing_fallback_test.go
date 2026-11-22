package server

import (
	"bytes"
	"codearts2api/internal/access"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDefaultPriceChatSettlement(t *testing.T) {
	for _, id := range []string{"kimi-k3", "qwen3.8-flash", "gpt-6.1-sol"} {
		t.Run(id, func(t *testing.T) {
			var server *Server
			var called int
			server, _ = responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				called++

				// A concurrent price edit after admission must not change this charge.
				if err := server.Store.SetDefaultPriceAndSave(nil); err != nil {
					t.Fatal(err)
				}
				data := `{"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":100,"prompt_tokens_details":{"cached_tokens":200}}}`
				return &http.Response{StatusCode: 200, Request: r, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(data))}, nil
			})
			server.Reg.Replace([]provider.Model{{ID: id}})
			group, err := server.Store.SaveGroup(storage.AccountGroup{Name: "metered", AccountIDs: []string{"a"}, ModelMappings: []storage.ModelMapping{{Name: id, Sources: []string{id}}}})
			if err != nil {
				t.Fatal(err)
			}
			user := createTestUser(t, server, "metered", group.ID)
			if _, err = server.Store.SaveUser(access.UserUpdate{ID: user.ID, Username: user.Username, GroupID: group.ID, Budget: &access.KeyBudget{MaxUSD: 1}}, ""); err != nil {
				t.Fatal(err)
			}
			key, err := server.Store.AddUserKeyAndSave(user.ID, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			if err = server.Store.AdjustWallet(user.ID, 0, 1000000000, true, false, "fixture"); err != nil {
				t.Fatal(err)
			}
			call := func() *httptest.ResponseRecorder {
				return responseRequest(server, key.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, id))
			}
			if w := call(); w.Code != 402 || !strings.Contains(w.Body.String(), "缺少价格") || called != 0 {
				t.Fatal(w.Code, w.Body.String(), called)
			}
			price := storage.ModelPrice{Currency: "USD", Input: 2, Output: 6, Cached: 1}
			if err = server.Store.SetDefaultPriceAndSave(&price); err != nil {
				t.Fatal(err)
			}
			if w := call(); w.Code != 200 || !strings.Contains(w.Body.String(), "Hello") {
				t.Fatal(w.Code, w.Body.String())
			}
			log := server.Store.Logs(1)[0]
			u, _ := server.Store.User(user.ID)
			want, _ := access.CostCredits(0.0024)
			if log.EstimatedCost == nil || log.EstimatedCost.PriceKind != "default" || math.Abs(log.EstimatedCost.Amount-.0024) > 1e-12 || log.Model != id || u.Wallet.Balance != 1000000000-want || math.Abs(u.BudgetUsage.USD-.0024) > 1e-12 || server.Store.UserReservedCredits(u.ID) != 0 {
				t.Fatalf("settlement model=%s cost=%+v wallet=%+v budget=%+v", log.Model, log.EstimatedCost, u.Wallet, u.BudgetUsage)
			}
			history := server.Store.QueryUserUsage(user.ID, storage.UsageQuery{})
			if len(history.Logs) == 0 || history.Logs[0].EstimatedCost == nil || history.Logs[0].EstimatedCost.PriceKind != "default" {
				t.Fatal("consumer history lost price source", history.Logs)
			}
			if w := call(); w.Code != 402 || called != 1 {
				t.Fatal(w.Code, called)
			}
		})
	}
}

func TestDefaultPricingBrowser(t *testing.T) {
	if os.Getenv("PANEL_PRICING_BROWSER") != "1" {
		t.Skip("set PANEL_PRICING_BROWSER=1 after the frontend build")
	}
	s, _, _ := mappedFixture(t, nil)
	cookie := fixtureAdminCookie(s)
	host := httptest.NewServer(s.Handler())
	defer host.Close()
	payload, err := json.Marshal(map[string]string{"url": host.URL, "cookieName": cookie.Name, "cookieValue": cookie.Value, "screenshot": os.Getenv("PANEL_PRICING_SCREENSHOT")})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "node", "../../../scripts/pricing-fallback-browser.cjs")
	cmd.Stdin = bytes.NewReader(payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	}
}

func TestDefaultPriceDoesNotGrantAccess(t *testing.T) {
	s, _, g := mappedFixture(t, nil)
	g.AccountIDs = []string{"wb-cn"}
	g.ModelIDs = []string{"workbuddy/cn/shared"}
	g.ModelMappings = []storage.ModelMapping{{Name: mappedPublic, Sources: g.ModelIDs}}
	if _, err := s.Store.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	u := createTestUser(t, s, "metered", g.ID)
	k, err := s.Store.AddUserKeyAndSave(u.ID, "metered")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SetDefaultPriceAndSave(&storage.ModelPrice{Currency: "USD", Input: 1, Output: 1}); err != nil {
		t.Fatal(err)
	}
	call := func(model string) *httptest.ResponseRecorder {
		return responseRequest(s, k.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, model))
	}
	if w := call("trae/cn/shared"); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, a := range s.Store.Accounts() {
		s.Store.SetAccountPaused(a.ID, true)
	}
	if w := call(mappedPublic); w.Code != 503 || !strings.Contains(w.Body.String(), "model_sources_unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
	onlyMappedPlatform(s, provider.WorkBuddy)
	if err = s.Store.AdjustWallet(u.ID, 0, 1, true, false, "tiny grant"); err != nil {
		t.Fatal(err)
	}
	if w := call(mappedPublic); w.Code != 402 || !strings.Contains(w.Body.String(), "不足") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDefaultPriceValidationAndAccess(t *testing.T) {
	s, _, g := mappedFixture(t, nil)
	createTestUser(t, s, "ordinary", g.ID)
	path := "/admin/api/pricing/default"
	body := `{"currency":"USD","input":0,"output":0,"cached":0}`
	if w := userHTTP(s, loginTestUser(t, s, "ordinary"), "PUT", path, body); w.Code != 403 {
		t.Fatal(w.Code)
	}
	admin := fixtureAdminCookie(s)
	for _, tc := range []struct {
		body string
		code int
	}{{`{"currency":"USD","input":0,"output":0}`, 400}, {body, 200}, {`{"currency":"USD","input":-1,"output":0,"cached":0}`, 400}} {
		if w := userHTTP(s, admin, "PUT", path, tc.body); w.Code != tc.code {
			t.Fatalf("%s => %d %s", tc.body, w.Code, w.Body.String())
		}
	}
	if p := s.Store.DefaultTokenPrice(); p == nil || p.Currency != "USD" || p.Input != 0 {
		t.Fatal(p)
	}
}

func TestDefaultPriceUnknownUsageNeedsReview(t *testing.T) {
	s, _, g := mappedFixture(t, func(platform string, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
	})
	onlyMappedPlatform(s, provider.CodeArts)
	u := createTestUser(t, s, "metered", g.ID)
	k, err := s.Store.AddUserKeyAndSave(u.ID, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	p := storage.ModelPrice{Currency: "USD", Input: 1}
	if err = s.Store.SetDefaultPriceAndSave(&p); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.AdjustWallet(u.ID, 0, 1000000000, true, false, "fixture"); err != nil {
		t.Fatal(err)
	}
	w := responseRequest(s, k.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	log := s.Store.Logs(1)[0]
	result, _ := s.Store.User(u.ID)
	if !result.Wallet.ReviewRequired || log.EstimatedCost != nil || log.Usage.Known {
		t.Fatal(log, result.Wallet)
	}
}
