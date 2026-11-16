package commandcode

import (
	"codearts2api/internal/accounts"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func rawKey() json.RawMessage { return json.RawMessage(`{"api_key":"fixture-key","scope":"global"}`) }

const modelsFixture = `{"data":[{"id":"deepseek/deepseek-v4-flash","supported_endpoints":["/v1/chat/completions","/v1/responses"]},{"id":"anthropic/claude-test","context_length":200000,"max_output_tokens":8192,"modalities":{"input":["text","image"]},"supported_endpoints":["/v1/messages"]},{"id":"openai/gpt-test","supported_endpoints":["/v1/responses"]},{"id":"typesafe/jev","supported_endpoints":["/v1/systemone"]},{"id":"missing-v1"},{"id":"wrong-v1","supported_endpoints":["https://evil.example/chat/completions"]}]}`

func TestCatalogAndCredentials(t *testing.T) {
	c := New()
	_, id, err := c.Normalize(rawKey())
	if err != nil || !id.NonExpiring || id.HasRefresh || id.Scope != "global" {
		t.Fatal(id, err)
	}
	_, other, err := c.Normalize([]byte(`{"api_key":"fixture-key","account_mode":"payg"}`))
	if err != nil || other.ID != id.ID {
		t.Fatal("same key duplicated across billing label", err)
	}
	for _, raw := range []string{`{"api_key":"fixture-key","scope":"go"}`, `{"api_key":"fixture-key","base_url":"https://evil.example"}`, `{"api_key":"fixture-key","refresh_token":"mixed"}`} {
		if _, _, err = c.Normalize([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider/v1/models" {
			t.Error(r.URL.Path)
		}
		io.WriteString(w, modelsFixture)
	}))
	defer s.Close()
	c.Origin = s.URL
	rows, err := c.Models(context.Background(), rawKey())
	if err != nil || len(rows) != 3 || rows[0].ID != "deepseek/deepseek-v4-flash" || rows[0].Context != 0 || rows[0].ImageInput != "" || rows[1].Context != 200000 || rows[1].MaxOutput != 8192 || rows[1].ImageInput != "supported" {
		t.Fatal(rows, err)
	}
	for _, bad := range []string{"a//b", "../model", "a/../model", "x\n"} {
		if validModel(bad) {
			t.Fatal(bad)
		}
	}
}
func TestVerifyRequiresIdentityAndGeneration(t *testing.T) {
	for _, stage := range []string{"identity", "generation", "success"} {
		t.Run(stage, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch r.URL.Path {
				case "/alpha/whoami":
					if stage == "identity" {
						w.WriteHeader(401)
						return
					}
					io.WriteString(w, `{"user":{"userName":"fixture"},"org":null}`)
				case "/provider/v1/models":
					io.WriteString(w, modelsFixture)
				case "/provider/v1/chat/completions":
					if stage == "generation" {
						w.WriteHeader(403)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, chatOK())
				default:
					t.Error(r.URL.Path)
				}
			}))
			defer s.Close()
			c := New()
			c.Origin = s.URL
			_, err := c.Verify(context.Background(), rawKey())
			if (err == nil) != (stage == "success") {
				t.Fatal(err)
			}
			if stage == "identity" && calls != 1 {
				t.Fatal("continued after failed auth", calls)
			}
		})
	}
}
func TestQuotaPersonalAndOrganization(t *testing.T) {
	for _, org := range []string{"", "org/a&b"} {
		t.Run(org, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/alpha/whoami":
					var organization any
					if org != "" {
						organization = map[string]string{"id": org}
					}
					json.NewEncoder(w).Encode(map[string]any{"org": organization})
				case "/alpha/billing/subscriptions":
					io.WriteString(w, `{"data":null}`)
				case "/alpha/billing/credits":
					if r.URL.Query().Get("orgId") != org || org == "" && r.URL.Query().Has("orgId") {
						t.Error("wrong org query")
					}
					io.WriteString(w, `{"credits":{"monthlyCredits":"34.9","purchasedCredits":0},"windowLimits":{"limited":true,"fiveHour":{"used":1.5,"cap":14,"resetAt":1790000000000},"weekly":{"used":35,"cap":35}}}`)
				default:
					t.Error(r.URL.Path)
				}
			}))
			defer s.Close()
			c := New()
			c.Origin = s.URL
			b, err := c.Credit(context.Background(), rawKey())
			var q accounts.CreditInfo
			json.Unmarshal(b, &q)
			if err != nil || !q.Available || len(q.Windows) != 4 || q.Windows[0].Total != nil || *q.Windows[1].Remaining != 0 || q.Windows[2].ResetsAt != 1790000000 || *q.Windows[3].Remaining != 0 {
				t.Fatal(string(b), err)
			}
		})
	}
}
func TestQuotaUnknownAndInvalid(t *testing.T) {
	for _, body := range []string{`{}`, `{"credits":null}`, `{"credits":{"monthlyCredits":-1}}`, `{"credits":{"monthlyCredits":"NaN"}}`, `{"credits":{},"windowLimits":{"limited":true,"weekly":{"cap":-1}}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "whoami") {
				io.WriteString(w, `{"org":null}`)
			} else {
				io.WriteString(w, body)
			}
		}))
		c := New()
		c.Origin = s.URL
		if _, err := c.Credit(context.Background(), rawKey()); err == nil {
			t.Error("accepted", body)
		}
		s.Close()
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "whoami") {
			io.WriteString(w, `{"org":null}`)
		} else {
			io.WriteString(w, `{"credits":{}}`)
		}
	}))
	defer s.Close()
	c := New()
	c.Origin = s.URL
	b, err := c.Credit(context.Background(), rawKey())
	var q accounts.CreditInfo
	json.Unmarshal(b, &q)
	if err != nil || q.Available {
		t.Fatal(string(b), err)
	}
}
