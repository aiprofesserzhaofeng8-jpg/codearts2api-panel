package kimi

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodingIsolationAndQuota(t *testing.T) {
	paths := []string{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing auth")
		}
		switch r.URL.Path {
		case "/coding/v1/models":
			io.WriteString(w, `{"data":[{"id":"kimi-for-coding"}]}`)
		case "/coding/v1/usages":
			io.WriteString(w, `{"limits":[{"detail":{"limit":"100","remaining":"0","resetTime":"2026-11-17T00:00:00Z"}}],"usage":{"limit":1000,"remaining":900}}`)
		case "/coding/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Error("wrong endpoint", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer up.Close()
	c := New()
	c.Base = up.URL + "/payg/v1"
	c.Coding.Base = up.URL + "/coding/v1"
	raw := json.RawMessage(`{"api_key":"fixture","scope":"coding-cn","account_mode":"coding"}`)
	normalized, id, err := c.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, old, err := c.Normalize(json.RawMessage(`{"api_key":"fixture"}`))
	if err != nil || id.ID == old.ID || id.Scope != "coding-cn" {
		t.Fatal("identity collision", err)
	}
	_, again, err := c.Normalize(normalized)
	if err != nil || again != id {
		t.Fatal("not stable", err)
	}
	for _, bad := range []string{`{"api_key":"fixture","scope":"cn","account_mode":"coding"}`, `{"api_key":"fixture","scope":"coding-cn","account_mode":"payg"}`, `{"api_key":"fixture","scope":"coding-cn","base_url":"https://evil.invalid"}`} {
		if _, _, err := c.Normalize([]byte(bad)); err == nil {
			t.Fatal("ambiguous mode accepted", bad)
		}
	}
	if _, err = c.Verify(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	models, err := c.Models(t.Context(), raw)
	if err != nil || len(models) != 1 || models[0].Scope != "coding-cn" || models[0].UpstreamID != "kimi-for-coding" {
		t.Fatal(models, err)
	}
	b, err := c.Credit(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var credit accounts.CreditInfo
	if json.Unmarshal(b, &credit) != nil || !credit.Available || len(credit.Windows) != 2 || *credit.Windows[0].Remaining != 0 || *credit.Windows[0].Utilization != 100 || *credit.Windows[1].Used != 100 {
		t.Fatal(string(b))
	}
	chunks := 0
	err = c.Stream(t.Context(), raw, provider.Request{Model: "kimi-for-coding", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { chunks++; return nil })
	if err != nil || chunks == 0 {
		t.Fatal(chunks, err)
	}
	if len(paths) != 4 {
		t.Fatal(paths)
	}
}
func TestCodingFailureNeverFallsBack(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(status) }))
			defer up.Close()
			c := New()
			c.Base = up.URL + "/forbidden-payg"
			c.Coding.Base = up.URL + "/coding"
			err := c.Stream(t.Context(), []byte(`{"api_key":"fixture","account_mode":"coding"}`), provider.Request{Model: "kimi-for-coding", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
			if err == nil || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}
func TestCodingQuotaUnknownAndMalformed(t *testing.T) {
	for _, body := range []string{`{}`, `{"usage":{}}`, `{"usage":{"limit":100,"remaining":-1}}`, `{"usage":{"limit":100,"remaining":101}}`, `{"usage":{"limit":"bad"}}`} {
		t.Run(body, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			defer up.Close()
			c := New()
			c.Coding.Base = up.URL
			b, err := c.Credit(t.Context(), []byte(`{"api_key":"fixture","account_mode":"coding"}`))
			if strings.Contains(body, "limit") {
				if err == nil {
					t.Fatal("invalid quota accepted", string(b))
				}
				return
			}
			var credit accounts.CreditInfo
			if err != nil || json.Unmarshal(b, &credit) != nil || credit.Available || len(credit.Windows) != 0 {
				t.Fatal(string(b), err)
			}
		})
	}
}
