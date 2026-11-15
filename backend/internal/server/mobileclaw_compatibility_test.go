package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/mobileclaw"
	"codearts2api/internal/provider"
)

// Both HTTP conversions and the MobileClaw transport run; only the upstream
// and credentials are synthetic. Public aliases must retain the exact source.
func TestMobileClawPublicAndRawProtocolAccounting(t *testing.T) {
	for _, public := range []bool{false, true} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("public=%v/%s/stream=%v", public, path, stream), func(t *testing.T) {
					calls := 0
					up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						var body map[string]json.RawMessage
						_ = json.NewDecoder(r.Body).Decode(&body)
						var options map[string]json.RawMessage
						_ = json.Unmarshal(body["stream_options"], &options)
						if r.URL.Path != "/desktop/v1/chat/completions" || r.Header.Get("pkg-types") != "FREE" || r.Header.Get("ai-provider") != "fixture-provider" || string(body["model"]) != `"fixture-model"` || string(options["include_usage"]) != "true" {
							t.Error("wrong upstream route or missing usage request")
						}
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"MC_FIXTURE_OK\"},\"finish_reason\":\"stop\"}]}\n\n")
						if string(options["include_usage"]) == "true" {
							io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\n")
						}
						io.WriteString(w, "data: [DONE]\n\n")
					}))
					defer up.Close()
					s := newAdminTestServer(t)
					c := mobileclaw.New()
					c.Origin = up.URL
					s.Providers = provider.NewRegistry()
					if err := s.Providers.Register(c); err != nil {
						t.Fatal(err)
					}
					claims := fmt.Sprintf(`{"uid":"fixture-user","aip":"fixture-provider","exp":%d}`, time.Now().Add(time.Hour).Unix())
					raw := mustJSON(mobileclaw.Credentials{Token: "e30." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature", UserID: "fixture-user", DeviceID: "fixture-device"})
					s.Store.UpsertAccount(&accounts.Account{ID: "mc", Provider: provider.MobileClaw, Scope: "cn", ExternalID: "fixture-user", Credentials: raw, Enabled: true})
					s.Reg.ReplaceProvider(provider.MobileClaw, "cn", []provider.Model{{ID: "fixture-model", Name: "fixture-model", Context: 32768, MaxOutput: 4096}})
					model := "mobileclaw/cn/fixture-model"
					if public {
						_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "shared-fixture-v1", Enabled: true, Sources: []config.ModelSource{{ID: model, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
						if err != nil {
							t.Fatal(err)
						}
						model = "shared-fixture-v1"
					}
					key := s.Store.AddKey("fixture").Key
					_ = s.Handler()
					markModelsFresh(s)
					body := map[string]any{"model": model, "stream": stream, "max_tokens": 64, "messages": []map[string]string{{"role": "user", "content": "fixture"}}}
					if path == "/v1/responses" {
						delete(body, "messages")
						delete(body, "max_tokens")
						body["input"] = "fixture"
						body["max_output_tokens"] = 64
					}
					w := responseRequest(s, key, path, string(mustJSON(body)))
					if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "MC_FIXTURE_OK") {
						t.Fatal(w.Code, calls, w.Body.String())
					}
					logs := s.Store.Logs(1)
					if len(logs) != 1 || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 || logs[0].Usage.CachedTokens != 3 || logs[0].Provider != provider.MobileClaw || logs[0].Model != "mobileclaw/cn/fixture-model" || s.Store.PoolSnapshot()["mc"].InFlight != 0 {
						t.Fatal("accounting/source/lease mismatch", logs)
					}
				})
			}
		}
	}
}
