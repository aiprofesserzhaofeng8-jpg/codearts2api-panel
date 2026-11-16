package server

import (
	"codearts2api/internal/accounts"

	"codearts2api/internal/config"

	"codearts2api/internal/grok"

	"codearts2api/internal/provider"
	"codearts2api/internal/storage"

	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGrokProtocolAccounting(t *testing.T) {
	for _, factory := range []func() *grok.Client{grok.New} {
		t.Run(factory().Describe().ID, func(t *testing.T) {
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
								if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer "+grokServerToken() || string(body["model"]) != `"fixture-model"` {
									t.Error("wrong upstream route or missing usage request")
								}
								w.Header().Set("Content-Type", "text/event-stream")
								io.WriteString(w, "data: "+`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"API_FIXTURE_OK"}]}],"usage":{"input_tokens":12,"output_tokens":4,"input_tokens_details":{"cached_tokens":3}}}}`+"\n\n")
							}))
							defer up.Close()
							s := newAdminTestServer(t)
							c := factory()
							c.BaseURL = up.URL
							s.Providers = provider.NewRegistry()
							if err := s.Providers.Register(c); err != nil {
								t.Fatal(err)
							}
							raw := json.RawMessage(fmt.Sprintf(`{"access_token":"%s","refresh_token":"fixture-refresh","project_id":"fixture-project","scope":"build","expires_at":%d}`, grokServerToken(), time.Now().Unix()+3600))

							_, identity, err := c.Normalize(raw)
							if err != nil {
								t.Fatal(err)
							}
							s.Store.UpsertAccount(&accounts.Account{ID: "mc", Provider: c.Describe().ID, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true})
							s.Reg.ReplaceProvider(c.Describe().ID, identity.Scope, []provider.Model{{ID: "fixture-model", Name: "fixture-model", Context: 32768, MaxOutput: 4096}})
							model := c.Describe().ID + "/" + identity.Scope + "/fixture-model"
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
							if len(logs) != 1 || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 || logs[0].Usage.CachedTokens != 3 || logs[0].Provider != c.Describe().ID || logs[0].Model != c.Describe().ID+"/"+identity.Scope+"/fixture-model" || s.Store.PoolSnapshot()["mc"].InFlight != 0 {
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

func grokServerToken() string {
	b, _ := json.Marshal(map[string]any{"sub": "fixture-user", "exp": time.Now().Unix() + 3600})
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".fixture"
}
