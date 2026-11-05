package teleagent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func dailyFixture(claimed bool, total int) string {
	return fmt.Sprintf(`{"code":200,"data":{"daily_task":{"visible":true,"claimed_today":%v,"daily_claimable_points":"100","total_claimed_points":%d},"activity":{"visible":true,"activity_title":"邀请共享积分"}}}`, claimed, total)
}

func TestDailyRewardStatusAndConfirmedClaim(t *testing.T) {
	for _, portal := range []bool{false, true} {
		t.Run(fmt.Sprint(portal), func(t *testing.T) {
			claims, statuses := 0, 0
			claimed := false
			a := fixture()
			if portal {
				a.OperationToken = "portal-fixture"
				a.OperToken = "oper-fixture"
			}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("Authorization") != "" {
					t.Error("wrong reward transport")
				}
				if portal {
					if !strings.HasPrefix(r.URL.Path, "/meter/") || r.Header.Get("X-Token") != a.OperationToken || !strings.Contains(r.Header.Get("Cookie"), "operationToken=portal-fixture") {
						t.Error("portal auth mixed")
					}
				} else if strings.HasPrefix(r.URL.Path, "/meter/") || r.Header.Get("X-Token") != a.Token || r.Header.Get("Cookie") != "" {
					t.Error("account auth mixed")
				}
				if strings.HasSuffix(r.URL.Path, "/checkTask") {
					if r.URL.RawQuery != "type=daily_login" {
						t.Error("unsafe generic reward request")
					}
					claims++
					claimed = true
					io.WriteString(w, `{"code":200,"data":[]}`)
				} else if strings.HasSuffix(r.URL.Path, "/v2/checkDayTask") {
					statuses++
					total := 200
					if claimed {
						total = 300
					}
					io.WriteString(w, dailyFixture(claimed, total))
				} else {
					t.Error("unexpected endpoint")
				}
			}))
			defer up.Close()
			c := New()
			c.Origin = up.URL
			status, err := c.DailyStatus(context.Background(), raw(a))
			if err != nil || status.State != "claimable" || status.Credits == nil || *status.Credits != 100 || len(status.Notices) != 1 || claims != 0 {
				t.Fatal("bad status", status, err)
			}
			result, err := c.ClaimDaily(context.Background(), raw(a))
			if err != nil || result.State != "claimed" || result.Credits == nil || *result.Credits != 100 || claims != 1 || statuses != 3 {
				t.Fatal("claim not verified", result, err, claims, statuses)
			}
			result, err = c.ClaimDaily(context.Background(), raw(a))
			if err != nil || result.State != "already_claimed" || claims != 1 {
				t.Fatal("duplicate claim")
			}
		})
	}
}

func TestDailyRewardMissingUnavailableOrFailedStatusNeverClaims(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		failure          bool
	}{
		{"missing", `{"code":200,"data":{}}`, "", true},
		{"missing_claimed", `{"data":{"daily_task":{"daily_claimable_points":100}}}`, "", true},
		{"null_claimed", `{"data":{"daily_task":{"claimed_today":null,"daily_claimable_points":100}}}`, "", true},
		{"business_denied", `{"code":40109,"data":{"daily_task":{"claimed_today":true}}}`, "", true},
		{"hidden", `{"data":{"daily_task":{"visible":false,"claimed_today":false,"daily_claimable_points":100}}}`, "unavailable", false},
		{"zero", `{"data":{"daily_task":{"claimed_today":false,"daily_claimable_points":0}}}`, "unavailable", false},
		{"unknown_points", `{"data":{"daily_task":{"claimed_today":false}}}`, "unknown", false},
		{"negative", `{"data":{"daily_task":{"claimed_today":false,"daily_claimable_points":-1}}}`, "", true},
		{"identity", `{"data":{"accountId":"other","daily_task":{"claimed_today":false,"daily_claimable_points":100}}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != DailyStatusPath {
					t.Error("unexpected mutation")
				}
				io.WriteString(w, tc.body)
			}))
			defer up.Close()
			c := New()
			c.Origin = up.URL
			result, err := c.ClaimDaily(context.Background(), raw(fixture()))
			if (err != nil) != tc.failure || result.State != tc.want || calls != 1 {
				t.Fatal(result, err, calls)
			}
		})
	}
}

func TestDailyRewardDoesNotTreatSubmitAsReceipt(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		claimCode, statusCode int
		claimed               bool
		want                  string
		failure               bool
	}{
		{"unchanged", 200, 200, false, "unknown", false},
		{"recheck_failed", 200, 503, false, "", true},
		{"rate_limited", 429, 200, false, "", true},
		{"failed_but_confirmed", 503, 200, true, "already_claimed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims, statuses := 0, 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == DailyStatusPath {
					statuses++
					if statuses > 1 {
						w.WriteHeader(tc.statusCode)
						io.WriteString(w, dailyFixture(tc.claimed, 300))
					} else {
						io.WriteString(w, dailyFixture(false, 200))
					}
					return
				}
				claims++
				w.WriteHeader(tc.claimCode)
				io.WriteString(w, `{"code":200,"data":[]}`)
			}))
			defer up.Close()
			c := New()
			c.Origin = up.URL
			result, err := c.ClaimDaily(context.Background(), raw(fixture()))
			if (err != nil) != tc.failure || result.State != tc.want || claims != 1 || statuses != 2 {
				t.Fatal(result, err, claims, statuses)
			}
		})
	}
}

func TestDailyRewardCancellationAndRedirectDoNotReplay(t *testing.T) {
	claims := 0
	ctx, cancel := context.WithCancel(context.Background())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == DailyStatusPath {
			io.WriteString(w, dailyFixture(false, 200))
			return
		}
		claims++
		cancel()
		io.WriteString(w, `{"code":200}`)
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	if _, err := c.ClaimDaily(ctx, raw(fixture())); err != context.Canceled || claims != 1 {
		t.Fatal("cancellation or attempts changed", err, claims)
	}
	redirects := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++ }))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) }))
	defer origin.Close()
	c.Origin = origin.URL
	if _, err := c.ClaimDaily(context.Background(), raw(fixture())); err == nil || redirects != 0 {
		t.Fatal("credential redirect followed")
	}
}
