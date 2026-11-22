package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/commandcode"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandCodeProtocolAccounting(t *testing.T) {
	for _, scope := range []string{"global"} {
		t.Run(scope, func(t *testing.T) {
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
								if r.URL.Path != "/provider/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" || string(body["model"]) != `"deepseek/deepseek-v4-flash"` || string(options["include_usage"]) != "true" {
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
							c := commandcode.New()
							c.Origin = up.URL
							s.Providers = provider.NewRegistry()
							if err := s.Providers.Register(c); err != nil {
								t.Fatal(err)
							}
							raw := json.RawMessage(`{"api_key":"fixture-key","scope":"` + scope + `"}`)

							_, identity, err := c.Normalize(raw)
							if err != nil {
								t.Fatal(err)
							}
							s.Store.UpsertAccount(&accounts.Account{ID: "mc", Provider: provider.CommandCode, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
							s.Reg.ReplaceProvider(provider.CommandCode, identity.Scope, []provider.Model{{ID: "deepseek/deepseek-v4-flash", Name: "deepseek/deepseek-v4-flash", Context: 32768, MaxOutput: 4096}})
							model := provider.CommandCode + "/" + identity.Scope + "/deepseek/deepseek-v4-flash"
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
							if len(logs) != 1 || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 || logs[0].Usage.CachedTokens != 3 || logs[0].Provider != provider.CommandCode || logs[0].Model != provider.CommandCode+"/"+identity.Scope+"/deepseek/deepseek-v4-flash" || s.Store.PoolSnapshot()["mc"].InFlight != 0 {
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

func TestCommandCodeVendorNamesJoinSharedModel(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "deepseek-v4-flash"}})
	s.Reg.ReplaceProvider(provider.CommandCode, "global", []provider.Model{{ID: "deepseek/deepseek-v4-flash"}, {ID: "unknown/deepseek-v4-flash"}, {ID: "deepseek/claude-test"}, {ID: "deepseek/deepseek-v4-flash-1017"}})
	if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range s.Store.UnifiedSnapshot().Models {
		if m.Name == "deepseek-v4-flash" {
			found = true
			if len(m.Sources) != 2 {
				t.Fatal("unreviewed vendor or version merged", m.Sources)
			}
		}
	}
	if !found {
		t.Fatal("missing shared model")
	}
}
