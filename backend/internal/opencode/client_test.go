package opencode

import (
	"codearts2api/internal/accounts"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func rawKey(scope string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"api_key": "fixture-key", "scope": scope})
	return b
}
func TestModesAreSeparateIdentities(t *testing.T) {
	c := New()
	_, zen, err := c.Normalize(rawKey("zen"))
	if err != nil {
		t.Fatal(err)
	}
	b, goID, err := c.Normalize(rawKey("go"))
	if err != nil || zen.ID == goID.ID || !goID.NonExpiring || goID.HasRefresh || !strings.Contains(string(b), "subscription") {
		t.Fatal(zen, goID, err)
	}
	for _, raw := range []string{`{"api_key":"key"}`, `{"api_key":"key","scope":"go","account_mode":"payg"}`, `{"api_key":"key","scope":"zen","base_url":"http://127.0.0.1"}`, `{"api_key":"key","scope":"go","access_token":"mixed"}`} {
		if _, _, err = c.Normalize([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if _, err = c.Refresh(context.Background(), b); err != nil {
		t.Fatal(err)
	}
}
func TestNativeProtocolSelection(t *testing.T) {
	for _, tc := range []struct {
		scope, id string
		want      protocol
	}{{"zen", "qwen3.8-max", chat}, {"go", "qwen3.8-max", messages}, {"zen", "minimax-m3", chat}, {"go", "minimax-m3", messages}, {"go", "minimax-m3.1-flash", chat}, {"zen", "claude-sonnet-5-5", messages}, {"go", "gpt-6-luna", responses}, {"go", "grok-4.7", responses}, {"go", "deepseek-v4.1-flash", chat}} {
		p, ok := modelProtocol(tc.scope, tc.id)
		if !ok || p != tc.want || protocolPath(p) == "" {
			t.Fatal(tc, p)
		}
	}
	for _, id := range []string{"gemini-4-argon", "jev-test"} {
		if _, ok := modelProtocol("zen", id); ok {
			t.Fatal("unsupported protocol published")
		}
	}
}
func TestPublicCatalogMetadataAndProtocolBoundaries(t *testing.T) {
	body := `{"data":[{"id":"deepseek-v4.1-flash","name":"DeepSeek","type":"chat","limit":{"context":1000000,"output":384000},"modalities":{"input":["text","image/png"],"output":["text"]},"capabilities":{"tools":{"supported":true}},"api":{"chat-completions":{"base_url":"http://127.0.0.1/private"}}},{"id":"unknown-v1","type":"chat","api":{"chat-completions":{}}},{"id":"protocol-changed","type":"chat","api":{"messages":{}}},{"id":"big-pickle","type":"chat","cost":{"text":{"input":{"price":0},"output":{"price":0}}},"api":{"chat-completions":{}}},{"id":"gemini-4-argon","type":"chat","api":{"generate-content":{}}}]}`
	paths := []string{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("missing auth")
		}
		w.Write([]byte(body))
	}))
	defer s.Close()
	c := New()
	c.Origin = s.URL
	models, err := c.Models(context.Background(), rawKey("zen"))
	if err != nil || len(models) != 2 || models[0].Context != 1000000 || models[0].ImageInput != "supported" || models[0].ToolCalling != "supported" || models[1].ImageInput != "" {
		t.Fatal(models, err)
	}
	models, err = c.Models(context.Background(), rawKey("go"))
	if err != nil || len(models) != 3 || paths[0] != "/v1/models" || paths[1] != "/go/v1/models" {
		t.Fatal(models, paths, err)
	}
	for _, bad := range []string{`{}`, `{"data":null}`, `{"data":[{"id":"test","limit":{"context":-1}}]}`} {
		body = bad
		if _, err = c.Models(context.Background(), rawKey("go")); err == nil {
			t.Fatal("bad catalog accepted")
		}
	}
}
func TestUsagePreservesUnknownAndZero(t *testing.T) {
	body := `{"usage":{"rolling":{"status":"ok","percent":0,"resetsAt":"2026-11-16T10:00:00Z"},"weekly":{"percent":25},"monthly":{}}}`
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/go/v1/usage" {
			t.Error("wrong usage route")
		}
		w.Write([]byte(body))
	}))
	defer s.Close()
	c := New()
	c.Origin = s.URL
	b, err := c.Credit(context.Background(), rawKey("go"))
	var credit accounts.CreditInfo
	json.Unmarshal(b, &credit)
	if err != nil || !credit.Available || len(credit.Windows) != 2 || *credit.Windows[0].Utilization != 0 || credit.Windows[0].ResetsAt == 0 {
		t.Fatal(string(b), err)
	}
	if _, err = c.Credit(context.Background(), rawKey("zen")); err != nil || calls != 1 {
		t.Fatal("queried Go quota with Zen account")
	}
	body = `{"monthly":{}}`
	b, err = c.Credit(context.Background(), rawKey("go"))
	credit = accounts.CreditInfo{}
	json.Unmarshal(b, &credit)
	if err != nil || credit.Available {
		t.Fatal("missing became zero")
	}
	for _, bad := range []string{`{"usage":{"rolling":{"percent":-1}}}`, `{"usage":{"rolling":{"percent":"NaN"}}}`, `{"usage":{"rolling":{"percent":0,"resetsAt":"bad"}}}`} {
		body = bad
		if _, err = c.Credit(context.Background(), rawKey("go")); err == nil {
			t.Fatal("invalid quota accepted")
		}
	}
}
func TestHTTPErrorsNotCatalogOrSubscriptionProof(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 503, 302} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "http://127.0.0.1:1/private")
			w.WriteHeader(status)
			w.Write([]byte(`{"error":"secret"}`))
		}))
		c := New()
		c.Origin = s.URL
		if _, err := c.Models(context.Background(), rawKey("go")); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal(status, err)
		}
		s.Close()
	}
}

func TestObservedPublicCatalogSnapshots(t *testing.T) {
	for _, scope := range []string{"zen", "go"} {
		t.Run(scope, func(t *testing.T) {
			b, err := os.ReadFile("testdata/" + scope + "-models-20261116.json")
			if err != nil {
				t.Fatal(err)
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
			defer s.Close()
			c := New()
			c.Origin = s.URL
			models, err := c.Models(context.Background(), rawKey(scope))
			if err != nil || len(models) == 0 {
				t.Fatal(err)
			}
			var qwen, mini bool
			for _, m := range models {
				if m.ID == "qwen3.8-max" {
					qwen = true
				}
				if m.ID == "minimax-m3" {
					mini = true
				}
				if strings.HasPrefix(m.ID, "gemini-") {
					t.Fatal("unimplemented protocol advertised")
				}
			}
			if !qwen || !mini {
				t.Fatal("lost native protocol variants")
			}
			t.Logf("%s supported-route metadata entries: %d (not account entitlement)", scope, len(models))
		})
	}
}
