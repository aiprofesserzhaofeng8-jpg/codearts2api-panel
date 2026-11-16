package cline

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func rawKey() json.RawMessage { return json.RawMessage(`{"api_key":"fixture-key"}`) }
func TestIdentityAndPublicCatalog(t *testing.T) {
	c := New()
	_, id, err := c.Normalize(rawKey())
	if err != nil || id.Scope != "global" || !id.NonExpiring || id.HasRefresh {
		t.Fatal(id, err)
	}
	for _, b := range []string{`{"api_key":"key","access_token":"mixed"}`, `{"api_key":"key","scope":"cn"}`, `{"api_key":"key","base_url":"https://evil.example"}`} {
		if _, _, err = c.Normalize([]byte(b)); err == nil {
			t.Fatal(b)
		}
	}
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/api/v1/models" {
			io.WriteString(w, `{"data":[{"id":"anthropic/claude-test"},{"id":"cline-pass/glm-test"},{"id":"cline-free/test"}]}`)
			return
		}
		w.WriteHeader(401)
	}))
	defer s.Close()
	c.Origin = s.URL
	rows, err := c.Models(context.Background(), rawKey())
	if err != nil || len(rows) != 3 || rows[0].Context != 0 || rows[0].ImageInput != "" {
		t.Fatal(rows, err)
	}
	if _, err = c.Verify(context.Background(), rawKey()); err == nil || calls != 2 {
		t.Fatal("public catalog treated as authentication", err, calls)
	}
}
func TestAccountEnvelopeAndNoInferenceOnVerify(t *testing.T) {
	for _, body := range []string{`{"success":true,"data":{"id":"fixture-user"}}`, `{"id":"fixture-user"}`, `{"success":false,"error":null,"data":{"id":"fixture-user"}}`, `{"success":true,"data":null}`, `{}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/api/v1/users/me" {
				t.Error("unexpected inference")
			}
			io.WriteString(w, body)
		}))
		c := New()
		c.Origin = s.URL
		_, err := c.Verify(context.Background(), rawKey())
		want := body == `{"success":true,"data":{"id":"fixture-user"}}` || body == `{"id":"fixture-user"}`
		if (err == nil) != want {
			t.Error(body, err)
		}
		s.Close()
	}
}
func TestWalletsAndExactMicrocredits(t *testing.T) {
	for _, status := range []int{200, 404, 401} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("wrong auth")
				}
				switch r.URL.Path {
				case "/api/v1/users/me":
					io.WriteString(w, `{"success":true,"data":{"id":"user-fixture"}}`)
				case "/api/v1/users/user-fixture/balance":
					io.WriteString(w, `{"success":true,"data":{"balance":"9007199254740993","userId":"user-fixture"}}`)
				case "/api/v1/users/me/plan/usage-limits":
					w.WriteHeader(status)
					if status == 200 {
						io.WriteString(w, `{"success":true,"data":{"limits":[{"type":"five_hour","percentUsed":0},{"type":"weekly","percentUsed":85,"resetsAt":"2026-12-01T00:00:00Z"},{"type":"monthly"}]}}`)
					}
				default:
					t.Error(r.URL.Path)
				}
			}))
			defer s.Close()
			c := New()
			c.Origin = s.URL
			b, err := c.Credit(context.Background(), rawKey())
			if status == 401 {
				if err == nil {
					t.Fatal("hidden unauthorized")
				}
				return
			}
			var q accounts.CreditInfo
			json.Unmarshal(b, &q)
			if err != nil || q.Balance == nil || *q.Balance.Amount != "9007199254.740993" {
				t.Fatal(string(b), err)
			}
			if status == 200 && (len(q.Windows) != 2 || *q.Windows[0].Utilization != 0 || q.Windows[0].ResetsAt != 0 || q.Windows[1].WindowSeconds != 604800) {
				t.Fatal(string(b))
			}
			if status == 404 && len(q.Windows) != 0 {
				t.Fatal("fake subscription")
			}
		})
	}
}
func TestStreamUsesChatAndPreservesWalletModel(t *testing.T) {
	for _, model := range []string{"anthropic/claude-test", "openai/gpt-test", "cline-pass/glm-test", "cline-free/preview"} {
		t.Run(model, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/chat/completions" {
					t.Error(r.URL.Path)
				}
				var b map[string]json.RawMessage
				json.NewDecoder(r.Body).Decode(&b)
				if string(b["model"]) != `"`+model+`"` || !strings.Contains(string(b["messages"]), "aGVsbG8=") || !strings.Contains(string(b["tools"]), "read_file") {
					t.Error("lost request data")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer s.Close()
			c := New()
			c.Origin = s.URL
			err := c.Stream(context.Background(), rawKey(), provider.Request{Model: model, Body: []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]}`)}, func([]byte) error { return nil })
			if err != nil || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}
func TestRealPublicCatalogFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/public-models-20261116.json")
	if err != nil {
		t.Fatal(err)
	}
	var original struct {
		Data []json.RawMessage `json:"data"`
	}
	json.Unmarshal(b, &original)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
	defer s.Close()
	c := New()
	c.Origin = s.URL
	rows, err := c.Models(context.Background(), rawKey())
	if err != nil || len(rows) != len(original.Data) {
		t.Fatal(len(rows), len(original.Data), err)
	}
}

func TestStreamFailureAndCancellation(t *testing.T) {
	for _, wire := range []string{"", "data: [DONE]\n\n", "data: {\"error\":{}}\n\n", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, wire)
		}))
		c := New()
		c.Origin = s.URL
		if err := c.Stream(context.Background(), rawKey(), provider.Request{Model: "deepseek-v4.1-flash", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil }); err == nil {
			t.Error("accepted incomplete stream")
		}
		s.Close()
	}
	c := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Stream(ctx, rawKey(), provider.Request{Model: "deepseek-v4.1-flash", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
