package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/cnapi"
	"codearts2api/internal/config"
	"codearts2api/internal/deepseek"
	"codearts2api/internal/gemini"
	"codearts2api/internal/kimi"
	"codearts2api/internal/minimax"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/zhipu"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficialAPIProtocolAccounting(t *testing.T) {
	for _, factory := range []func() *cnapi.Client{deepseek.New, zhipu.New, kimi.New, func() *cnapi.Client { return kimi.New().Coding }, minimax.New, func() *cnapi.Client { return zhipu.New().Coding }, func() *cnapi.Client { return minimax.New().Coding }, func() *cnapi.Client { return gemini.New().Client }} {
		t.Run(factory().ID+factory().Scope, func(t *testing.T) {
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
								if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" || string(body["model"]) != `"fixture-model"` || string(options["include_usage"]) != "true" {
									t.Error("wrong upstream route or missing usage request")
								}
								w.Header().Set("Content-Type", "text/event-stream")
								io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"API_FIXTURE_OK\"},\"finish_reason\":\"stop\"}]}\n\n")
								if string(options["include_usage"]) == "true" {
									io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\n")
								}
								io.WriteString(w, "data: [DONE]\n\n")
							}))
							defer up.Close()
							s := adminTestServerWithKeys(t, []string{"fixture"})
							c := factory()
							c.Base = up.URL
							s.Providers = provider.NewRegistry()
							if err := s.Providers.Register(c); err != nil {
								t.Fatal(err)
							}
							raw := json.RawMessage(`{"api_key":"fixture-key"}`)

							_, identity, err := c.Normalize(raw)
							if err != nil {
								t.Fatal(err)
							}
							s.Store.UpsertAccount(&accounts.Account{ID: "mc", Provider: c.ID, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
							s.Reg.ReplaceProvider(c.ID, identity.Scope, []provider.Model{{ID: "fixture-model", Name: "fixture-model", Context: 32768, MaxOutput: 4096}})
							model := c.ID + "/" + identity.Scope + "/fixture-model"
							if public {
								_, err := s.Store.SaveUnifiedModel(config.UnifiedModel{Name: "shared-fixture-v1", Enabled: true, Sources: []config.ModelSource{{ID: model, Enabled: true}}}, nil, 0, s.Store.UnifiedSnapshot().Revision, nil)
								if err != nil {
									t.Fatal(err)
								}
								model = "shared-fixture-v1"
							}
							grantFixtureAccounts(t, s.Store)
							key := s.Store.Keys()[0].Key
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
							if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "API_FIXTURE_OK") {
								t.Fatal(w.Code, calls, w.Body.String())
							}
							logs := s.Store.Logs(1)
							if len(logs) != 1 || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 || logs[0].Usage.CachedTokens != 3 || logs[0].Provider != c.ID || logs[0].Model != c.ID+"/"+identity.Scope+"/fixture-model" || s.Store.PoolSnapshot()["mc"].InFlight != 0 {
								t.Fatal("accounting/source/lease mismatch", logs)
							}
							group, err := s.Store.SaveGroup(storage.AccountGroup{Name: "empty restricted group", AccountIDs: []string{}})
							if err != nil {
								t.Fatal(err)
							}
							if err = s.Store.SetKeyGroupAndSave(key, group.ID); err != nil {
								t.Fatal(err)
							}
							blocked := responseRequest(s, key, path, string(mustJSON(body)))
							if blocked.Code != 403 || calls != 1 {
								t.Fatal("group bypass", blocked.Code, calls)
							}

						})
					}
				}
			}

		})
	}
}
