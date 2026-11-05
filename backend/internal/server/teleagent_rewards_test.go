package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/storage"
	"codearts2api/internal/teleagent"
)

func teleagentRewardServer(t *testing.T, confirmed bool) (*Server, string, *atomic.Int32) {
	t.Helper()
	var claims atomic.Int32
	s, key, g, _ := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case teleagent.DailyStatusPath:
			claimed := confirmed && claims.Load() > 0
			io.WriteString(w, fmt.Sprintf(`{"code":200,"data":{"daily_task":{"visible":true,"claimed_today":%v,"daily_claimable_points":100,"total_claimed_points":%d},"activity":{"visible":true,"activity_title":"邀请共享积分"}}}`, claimed, claims.Load()*100))
		case "/superCowork/sapi/user/portal/companyStore/checkTask":
			if r.URL.RawQuery != "type=daily_login" {
				t.Error("unscoped reward mutation")
			}
			claims.Add(1)
			io.WriteString(w, `{"code":200,"data":[]}`)
		case teleagent.CreditPath:
			io.WriteString(w, fmt.Sprintf(`{"code":200,"data":{"current_points_balance":%d}}`, 50+claims.Load()*100))
		default:
			t.Error("unexpected reward request", r.URL.Path)
		}
	})
	c := teleagent.New()
	c.Origin = strings.TrimSuffix(g.API, "/openai/v1")
	if err := s.Providers.Register(c); err != nil {
		t.Fatal(err)
	}
	raw, _, err := c.Normalize(mustJSON(teleagentFixture()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.UpsertAccountAndSave(&accounts.Account{ID: "ta-reward", Name: "TeleAgent fixture", Provider: "teleagent", Scope: "cn", ExternalID: "teleagent-fixture", Credentials: raw, Enabled: true, Source: "browser", Status: "ok"}, false)
	if err != nil {
		t.Fatal(err)
	}
	return s, key, &claims
}

func teleagentRewardRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.AddCookie(adminTestCookie(s, "admin-fixture"))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestTeleagentDailyWelfareClaimConcurrencyPersistenceAndBalance(t *testing.T) {
	s, key, claims := teleagentRewardServer(t, true)
	if !s.dailyRewardsSupported(s.Store.Account("ta-reward")) {
		t.Fatal("daily capability missing")
	}
	if w := responseRequest(s, key, "/admin/api/claim", `{"id":"ta-reward"}`); w.Code != 401 {
		t.Fatal("consumer claimed daily credits")
	}
	w := teleagentRewardRequest(s, "GET", "/admin/api/welfare?id=ta-reward", "")
	if w.Code != 200 || claims.Load() != 0 || !strings.Contains(w.Body.String(), `"claimable_count":1`) || !strings.Contains(w.Body.String(), `"type":"activity_notice"`) || !strings.Contains(w.Body.String(), `"benefit_amount":100`) {
		t.Fatal("welfare did not distinguish activity", w.Body.String())
	}
	w = teleagentRewardRequest(s, "GET", "/admin/api/credits", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remaining":50`) {
		t.Fatal("initial balance")
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := s.ClaimForAccount(context.Background(), s.Store.Account("ta-reward"), ""); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatal("duplicate submissions", claims.Load())
	}
	a := s.Store.Account("ta-reward")
	if a.LastClaimDate != claimDateFor(a, time.Now()) || a.LastClaimState != "claimed" || a.NextClaimAt != 0 {
		t.Fatal("not persisted")
	}
	reopened, err := storage.Open(s.Store.Path())
	if err != nil || reopened.Account(a.ID).LastClaimDate != a.LastClaimDate {
		t.Fatal("state reopening lost claim")
	}
	w = teleagentRewardRequest(s, "GET", "/admin/api/credits", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remaining":150`) {
		t.Fatal("stale balance after claim", w.Body.String())
	}
	if a.Failures != 0 || a.Status != "ok" {
		t.Fatal("reward changed inference health")
	}
}

func TestTeleagentDailyUnconfirmedWaitAndAutomaticToggle(t *testing.T) {
	s, _, claims := teleagentRewardServer(t, false)
	s.Store.UpdateSettings(func(c *storage.Settings) { c.AutoClaim = false })
	s.RunDailyClaim(context.Background())
	if claims.Load() != 0 {
		t.Fatal("disabled auto claim ran")
	}
	s.Store.UpdateSettings(func(c *storage.Settings) { c.AutoClaim = true; c.ClaimAfterHour = 0; c.ClaimAfterMinute = 0 })
	s.Store.SetAccountEnabled("ta-reward", false)
	s.RunDailyClaim(context.Background())
	if claims.Load() != 0 {
		t.Fatal("disabled account claimed")
	}
	s.Store.SetAccountEnabled("ta-reward", true)
	s.RunDailyClaim(context.Background())
	s.RunDailyClaim(context.Background())
	a := s.Store.Account("ta-reward")
	if claims.Load() != 1 || a.LastClaimDate != "" || a.LastClaimState != "unknown" || a.NextClaimAt <= time.Now().Unix() {
		t.Fatal("unconfirmed reward falsely recorded or retried", claims.Load(), a.LastClaimState)
	}
	if a.Failures != 0 || a.Status != "ok" {
		t.Fatal("reward failure changed inference availability")
	}
}
