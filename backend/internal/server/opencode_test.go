package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/upstream/opencode"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenCodeProtocolAccounting(t *testing.T) {
	for _, scope := range []string{"zen", "go"} {
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
								if r.URL.Path != func() string {
									if scope == "go" {
										return "/go/openai/v1/chat/completions"
									}
									return "/openai/v1/chat/completions"
								}() || r.Header.Get("Authorization") != "Bearer fixture-key" || string(body["model"]) != `"fixture-model"` || string(options["include_usage"]) != "true" {
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
							s := newAdminTestServer(t)
							c := opencode.New()
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
							s.Store.UpsertAccount(&accounts.Account{ID: "mc", Provider: provider.OpenCode, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
							s.Reg.ReplaceProvider(provider.OpenCode, identity.Scope, []provider.Model{{ID: "fixture-model", Name: "fixture-model", Context: 32768, MaxOutput: 4096}})
							model := provider.OpenCode + "/" + identity.Scope + "/fixture-model"
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
							if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "API_FIXTURE_OK") {
								t.Fatal(w.Code, calls, w.Body.String())
							}
							logs := s.Store.Logs(1)
							if len(logs) != 1 || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 || logs[0].Usage.CachedTokens != 3 || logs[0].Provider != provider.OpenCode || logs[0].Model != provider.OpenCode+"/"+identity.Scope+"/fixture-model" || s.Store.PoolSnapshot()["mc"].InFlight != 0 {
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

func TestOpenCodeScopesJoinSharedName(t *testing.T) {
	s, _ := unifiedAPIFixture(t)
	for _, scope := range []string{"zen", "go"} {
		s.Reg.ReplaceProvider(provider.OpenCode, scope, []provider.Model{{ID: "glm-5.3"}})
		if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
			t.Fatal(err)
		}
	}
	s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "glm-5.3"}})
	if err := s.UnifiedModels.SyncDefaultModels(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range s.Store.UnifiedSnapshot().Models {
		if m.Name != "glm-5.3" {
			continue
		}
		found = true
		seen := map[string]bool{}
		for _, src := range m.Sources {
			seen[src.ID] = true
		}
		if len(m.Sources) != 3 || !seen["workbuddy/cn/glm-5.3"] || !seen["opencode/zen/glm-5.3"] || !seen["opencode/go/glm-5.3"] {
			t.Fatal("modes conflated", m.Sources)
		}
	}
	if !found {
		t.Fatal("missing shared model")
	}
}
