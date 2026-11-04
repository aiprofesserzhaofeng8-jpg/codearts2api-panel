package chatgpt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codearts2api/internal/accounts"
)

func TestQuotaWindowsPreserveZeroDurationAndDecimalBalance(t *testing.T) {
	var o map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":604800,"reset_at":1794000000},"secondary_window":{"used_percent":75.25,"limit_window_seconds":18000,"reset_after_seconds":3600}},"additional_rate_limits":[{"limit_name":"Codex Spark","rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":14400},"secondary_window":null}}],"credits":{"balance":"9007199254740993.125","unlimited":false}}`), &o)
	b, e := parseQuota(o, 1793990000)
	if e != nil {
		t.Fatal(e)
	}
	var v accounts.CreditInfo
	_ = json.Unmarshal(b, &v)
	if !v.Available || len(v.Windows) != 3 || *v.Windows[0].Utilization != 0 || v.Windows[0].Name != "7 天窗口" || v.Windows[1].Name != "5 小时窗口" || v.Windows[1].ResetsAt != 1793993600 || v.Windows[2].Name != "Codex Spark · 4 小时窗口" || v.Balance == nil || *v.Balance.Amount != "9007199254740993.125" {
		t.Fatalf("quota metadata lost: %+v", v)
	}
	for _, s := range []string{`{}`, `{"rate_limit":{"primary_window":null,"secondary_window":{}}}`, `{"credits":{"balance":null,"has_credits":false}}`} {
		o = nil
		v = accounts.CreditInfo{}
		_ = json.Unmarshal([]byte(s), &o)
		b, e = parseQuota(o, 1)
		if e != nil {
			t.Fatal(e)
		}
		_ = json.Unmarshal(b, &v)
		if v.Available {
			t.Fatal("unknown became available")
		}
		o = nil
		v = accounts.CreditInfo{}
	}
	for _, s := range []string{`{"rate_limit":{"primary_window":{"used_percent":-1}}}`, `{"rate_limit":{"primary_window":{"used_percent":2,"reset_after_seconds":-1}}}`, `{"credits":{"balance":"not-a-number"}}`} {
		o = nil
		_ = json.Unmarshal([]byte(s), &o)
		if _, e = parseQuota(o, 1); e == nil {
			t.Fatal("invalid metric accepted")
		}
	}
}
func TestQuotaUsesOAuthWorkspaceAndNeverAPIKey(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/wham/usage" || r.Header.Get("Authorization") != "Bearer quota-fixture" || r.Header.Get("ChatGPT-Account-Id") != "workspace-fixture" || r.Header.Get("OpenAI-Beta") != "codex-1" {
			t.Error("wrong usage request")
		}
		w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000}}}`))
	}))
	defer up.Close()
	c := New()
	c.UsageURL = up.URL + "/wham/usage"
	b, _, e := c.Normalize(raw(Credentials{AccessToken: "quota-fixture", AccountID: "workspace-fixture", UserID: "user-fixture", ExpiresAt: time.Now().Add(time.Hour).Unix()}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Credit(context.Background(), b); e != nil {
		t.Fatal(e)
	}
	key, _, _ := c.Normalize([]byte(`{"api_key":"sk-fixture"}`))
	b, e = c.Credit(context.Background(), key)
	if e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	var v accounts.CreditInfo
	_ = json.Unmarshal(b, &v)
	if v.Kind != "unsupported" || v.Available {
		t.Fatal("API key became subscription quota")
	}
}
