package cnapi_test

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/cnapi"
	"codearts2api/internal/provider"
	"codearts2api/internal/upstream/minimax"
	"codearts2api/internal/upstream/zhipu"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDomesticCodingServiceIsolation(t *testing.T) {
	for _, factory := range []func() *cnapi.Client{zhipu.New, minimax.New} {
		t.Run(factory().ID, func(t *testing.T) {
			c := factory()
			calls := 0
			fail := false
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				expected := "Bearer fixture"
				if r.URL.Path == "/quota" && c.ID == "zhipu" {
					expected = "fixture"
				}
				if r.Header.Get("Authorization") != expected {
					t.Error("wrong auth", r.URL.Path)
				}
				switch r.URL.Path {
				case "/coding/models":
					w.WriteHeader(404)
				case "/coding/chat/completions":
					if fail {
						w.WriteHeader(403)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, goodStream)
				case "/quota":
					if c.ID == "zhipu" {
						io.WriteString(w, `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":0}]}}`)
					} else {
						io.WriteString(w, `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":0}]}`)
					}
				default:
					t.Error("wrong destination", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer up.Close()
			c.Base = up.URL + "/payg"
			c.Coding.Base = up.URL + "/coding"
			c.Coding.BalanceURL = up.URL + "/quota"
			raw := json.RawMessage(`{"api_key":"fixture","account_mode":"coding","scope":"coding-cn"}`)
			b, id, err := c.Normalize(raw)
			if err != nil {
				t.Fatal(err)
			}
			_, payg, err := c.Normalize([]byte(`{"api_key":"fixture"}`))
			if err != nil || id.ID == payg.ID || id.Scope != "coding-cn" {
				t.Fatal("mode collision", err)
			}
			_, stable, err := c.Normalize(b)
			if err != nil || stable != id {
				t.Fatal("unstable identity", err)
			}
			if _, err = c.Verify(t.Context(), raw); err != nil || calls != 2 {
				t.Fatal("static catalog must verify inference", calls, err)
			}
			models, err := c.Models(t.Context(), raw)
			if err != nil || len(models) == 0 || models[0].Scope != "coding-cn" {
				t.Fatal(models, err)
			}
			quota, err := c.Credit(t.Context(), raw)
			if err != nil {
				t.Fatal(err)
			}
			var info accounts.CreditInfo
			json.Unmarshal(quota, &info)
			if !info.Available || len(info.Windows) != 1 || info.Windows[0].Utilization == nil {
				t.Fatal(string(quota))
			}
			fail = true
			before := calls
			err = c.Stream(t.Context(), raw, provider.Request{Model: models[0].UpstreamID, Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
			if err == nil || calls != before+1 {
				t.Fatal("fallback or retry", err, calls)
			}
			for _, bad := range []string{`{"api_key":"fixture","account_mode":"coding","scope":"cn"}`, `{"api_key":"fixture","scope":"coding-cn","account_mode":"payg"}`, `{"api_key":"fixture","account_mode":"coding","zhipu_organization":"team"}`} {
				if _, _, err := c.Normalize([]byte(bad)); err == nil {
					t.Fatal("ambiguous/unsupported mode", bad)
				}
			}
		})
	}
}
