package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/kiro"
	"codearts2api/internal/qoder"
)

func TestProviderQuotaWindowsCreditsCacheAndAdminBoundary(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	var denied atomic.Int32
	s, key, g, a := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		if r.Method != "GET" {
			t.Error("quota query wrote upstream")
		}
		switch r.URL.Path {
		case "/wham/usage":
			if status := denied.Load(); status > 0 {
				w.WriteHeader(int(status))
				w.Write([]byte(`{"error":"private-quota-fixture"}`))
				return
			}
			w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":18000,"reset_after_seconds":3600},"secondary_window":{"used_percent":45,"limit_window_seconds":604800,"reset_after_seconds":86400}}}`))
		case "/api/oauth/usage":
			w.Write([]byte(`{"five_hour":{"utilization":20,"resets_at":"2026-11-06T00:00:00Z"},"seven_day":{"utilization":70}}`))
		case "/getUsageLimits":
			w.Write([]byte(`{"subscriptionInfo":{"subscriptionTitle":"KIRO PRO"},"usageBreakdownList":[{"currentUsageWithPrecision":15.5,"usageLimitWithPrecision":100}]}`))
		case "/api/v2/quota/usage":
			w.Write([]byte(`{"userQuota":{"total":300,"used":100,"remaining":200},"addOnQuota":{"total":50,"used":2.5,"remaining":47.5}}`))
		default:
			t.Error("unexpected upstream request", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	base := strings.TrimSuffix(g.API, "/openai/v1")
	g.UsageURL = base + "/wham/usage"
	addSubscription(t, s, g, false, time.Now().Add(time.Hour))
	addSubscription(t, s, a, false, time.Now().Add(time.Hour))
	k := kiro.New()
	for id, r := range k.Regions {
		r.Runtime = base
		k.Regions[id] = r
	}
	if e := s.Providers.Register(k); e != nil {
		t.Fatal(e)
	}
	addKiro(t, s, k, "us-east-1", false)
	q := qoder.New()
	for id, r := range q.Regions {
		r.OpenAPI = base
		q.Regions[id] = r
	}
	if e := s.Providers.Register(q); e != nil {
		t.Fatal(e)
	}
	addQoder(t, s, "cn")
	cookie := adminTestCookie(s, "admin-fixture")
	query := func(fresh bool) *httptest.ResponseRecorder {
		path := "/admin/api/credits"
		if fresh {
			path += "?fresh=1"
		}
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	unauth := httptest.NewRequest("GET", "/admin/api/credits", nil)
	unauth.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, unauth)
	if w.Code != 401 {
		t.Fatal("consumer key obtained upstream quotas")
	}
	before, e := os.ReadFile(s.Store.Path())
	if e != nil {
		t.Fatal(e)
	}
	accountsBefore, _ := json.Marshal(s.Store.Accounts())
	w = query(false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Accounts []struct {
			ID     string              `json:"id"`
			OK     bool                `json:"ok"`
			Credit accounts.CreditInfo `json:"credit"`
			Error  string              `json:"error"`
		} `json:"accounts"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if len(response.Accounts) != 4 {
		t.Fatal("quota rows lost")
	}
	for _, row := range response.Accounts {
		if !row.OK || !row.Credit.Available || len(row.Credit.Windows) == 0 {
			t.Fatal("provider quota unavailable", row.ID, row.Error)
		}
		switch row.ID {
		case "chatgpt-fixture":
			if row.Credit.Kind != "windows" || len(row.Credit.Windows) != 2 || row.Credit.Windows[0].Utilization == nil || *row.Credit.Windows[0].Utilization != 0 {
				t.Fatal("zero usage lost")
			}
		case "claude-fixture":
			if row.Credit.Kind != "windows" {
				t.Fatal("subscription became points")
			}
		case "kiro-us-east-1":
			if row.Credit.Remaining != 84.5 {
				t.Fatal("Kiro credits wrong")
			}
		case "qoder-cn":
			if row.Credit.Remaining != 247.5 {
				t.Fatal("Qoder credits wrong")
			}
		}
	}
	_ = query(false)
	mu.Lock()
	for _, n := range calls {
		if n != 1 {
			t.Error("cache bypassed", n)
		}
	}
	mu.Unlock()
	for _, status := range []int32{401, 403, 429} {
		denied.Store(status)
		w = query(true)
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-quota-fixture") {
			t.Fatal("quota failure leaked upstream response")
		}
		if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
			t.Fatal(e)
		}
		for _, row := range response.Accounts {
			if row.ID == "chatgpt-fixture" && (row.OK || row.Error == "") {
				t.Fatal("quota denial became zero usage")
			}
		}
	}
	after, _ := os.ReadFile(s.Store.Path())
	accountsAfter, _ := json.Marshal(s.Store.Accounts())
	if !bytes.Equal(before, after) || !bytes.Equal(accountsBefore, accountsAfter) || len(s.Store.Logs(10)) != 0 {
		t.Fatal("quota query mutated credentials, health, state or accounting")
	}
}
