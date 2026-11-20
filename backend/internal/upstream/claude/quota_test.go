package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codearts2api/internal/accounts"
)

func TestQuotaWindowsMissingAndZeroAreDifferent(t *testing.T) {
	var o map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"five_hour":{"utilization":0,"resets_at":"2026-11-05T00:00:00Z"},"seven_day":{"utilization":123.5,"resets_at":null},"seven_day_sonnet":null}`), &o)
	b, e := parseQuota(o, 1793990000)
	if e != nil {
		t.Fatal(e)
	}
	var v accounts.CreditInfo
	_ = json.Unmarshal(b, &v)
	if !v.Available || len(v.Windows) != 2 || *v.Windows[0].Utilization != 0 || v.Windows[0].ResetsAt <= 0 || *v.Windows[1].Utilization != 123.5 || v.Windows[1].ResetsAt != 0 {
		t.Fatalf("bad quota: %+v", v)
	}
	for _, s := range []string{`{}`, `{"five_hour":null,"seven_day":{}}`} {
		o = nil
		_ = json.Unmarshal([]byte(s), &o)
		b, e = parseQuota(o, 1)
		if e != nil {
			t.Fatal(e)
		}
		v = accounts.CreditInfo{}
		_ = json.Unmarshal(b, &v)
		if v.Available || len(v.Windows) > 0 {
			t.Fatal("missing window fabricated")
		}
	}
	o = nil
	_ = json.Unmarshal([]byte(`{"five_hour":{"utilization":-1}}`), &o)
	if _, e = parseQuota(o, 1); e == nil {
		t.Fatal("negative usage accepted")
	}
}
func TestQuotaOAuthEndpointAndAPIKeyUnsupported(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/oauth/usage" || r.Header.Get("Authorization") != "Bearer quota-fixture" || r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			t.Error("wrong usage request")
		}
		w.Write([]byte(`{"five_hour":{"utilization":30}}`))
	}))
	defer up.Close()
	c := New()
	c.API = up.URL
	b, _, e := c.Normalize(raw(Credentials{AccessToken: "quota-fixture", AccountUUID: "uuid-fixture", ExpiresAt: time.Now().Add(time.Hour).Unix()}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Credit(context.Background(), b); e != nil {
		t.Fatal(e)
	}
	key, _, _ := c.Normalize([]byte(`{"api_key":"sk-ant-fixture"}`))
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
