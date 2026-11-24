package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/mobileclaw"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/teleagent"
)

func TestDesktopNativeSearch(t *testing.T) {
	for _, name := range []string{provider.TeleAgent, provider.MobileClaw} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			status := 200
			body := `{"code":0,"data":{"references":[{"url":"javascript:bad","title":"bad"},{"url":"https://example.org/paper","title":"Paper","snippet":"Evidence"},{"url":"https://example.org/other","title":"Other"}]}}`
			if name == provider.MobileClaw {
				body = `{"errno":0,"items":[{"url":"javascript:bad"},{"url":"https://example.org/paper","title":"Paper","summary_ai":"Evidence"},{"url":"https://example.org/other"}]}`
			}
			s, _, up, _ := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if name == provider.TeleAgent {
					if r.Method != "POST" || r.URL.Path != teleagent.SearchPath || r.Header.Get("X-Token") == "" || r.Header.Get("X-Nonce") == "" || r.Header.Get("X-Timestamp") == "" || r.Header.Get("X-OS-Type") != "linux" {
						t.Error("incorrect TeleAgent search wire contract")
					}
					var payload struct {
						Messages []struct{ Role, Content string }
					}
					if json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "测试搜索" {
						t.Error("incorrect search messages")
					}
				} else {
					if r.Method != "GET" || r.URL.Path != mobileclaw.SearchPath || r.URL.Query().Get("q") != "测试搜索" || r.URL.Query().Get("count") != "1" || r.URL.Query().Get("sid") == "" || r.Header.Get("Service-Version") != mobileclaw.ServiceVersion || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
						t.Error("incorrect MobileClaw search wire contract")
					}
				}
				if status == 429 {
					w.Header().Set("Retry-After", "60")
				}
				w.WriteHeader(status)
				io.WriteString(w, body)
			})
			origin := strings.TrimSuffix(up.API, "/openai/v1")
			var adapter provider.Adapter
			var raw json.RawMessage
			if name == provider.TeleAgent {
				c := teleagent.New()
				c.Origin = origin
				adapter = c
				raw = mustJSON(teleagentFixture())
			} else {
				c := mobileclaw.New()
				c.Origin = origin
				adapter = c
				token := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"uid":"fixture-user","exp":2000000000}`)) + ".signature"
				raw = mustJSON(map[string]any{"token": token, "userId": "fixture-user", "deviceId": "fixture-device", "workspaceMode": "personal"})
			}
			normalized, id, err := adapter.Normalize(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Providers.Register(adapter); err != nil {
				t.Fatal(err)
			}
			_, err = s.Store.UpsertAccountAndSave(&accounts.Account{ID: name, Provider: name, Scope: "cn", ExternalID: id.ID, Enabled: true, Credentials: normalized}, false)
			if err != nil {
				t.Fatal(err)
			}
			target := name + "/cn/web-search"
			group, err := s.Store.SaveGroup(storage.AccountGroup{Name: "search", AccountIDs: []string{name}, AllowlistEnabled: true, ModelIDs: []string{target}})
			if err != nil {
				t.Fatal(err)
			}
			key, err := s.Store.AddKeyInGroupAndSave("search", group.ID)
			if err != nil {
				t.Fatal(err)
			}
			policy := config.DefaultProviderSettings(name)
			policy.SearchEnabled = true
			update := config.SettingsUpdate{}
			if name == provider.TeleAgent {
				update.TeleAgent = mustJSON(policy)
			} else {
				update.MobileClaw = mustJSON(policy)
			}
			if err = s.Configuration.UpdateSettings(update); err != nil {
				t.Fatal(err)
			}
			request := strings.Replace(searchBody, "workbuddy/cn/web-search", target, 1)
			request = strings.Replace(request, `"tools":`, `"max_results":1,"tools":`, 1)
			for _, route := range []string{"/v1/search/messages", "/v1/search/" + name + "/messages"} {
				w := responseRequest(s, key.Key, route, request)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "Evidence") || strings.Contains(w.Body.String(), "javascript:") || strings.Contains(w.Body.String(), "/other") {
					t.Fatalf("%s %d %s", route, w.Code, w.Body.String())
				}
			}
			log := s.Store.Logs(1)[0]
			if log.Provider != name || log.Protocol != "search" || log.Usage.Known || log.EstimatedCost != nil || s.Store.PoolSnapshot()[name].InFlight != 0 {
				t.Fatalf("incorrect search accounting %+v", log)
			}
			before := calls
			if w := responseRequest(s, "invalid", "/v1/search/"+name+"/messages", request); w.Code != 401 {
				t.Fatal("auth bypass")
			}
			mismatch := strings.Replace(request, target, "workbuddy/cn/web-search", 1)
			if w := responseRequest(s, key.Key, "/v1/search/"+name+"/messages", mismatch); w.Code != 400 {
				t.Fatal("cross-provider routing")
			}
			group.ModelIDs = []string{}
			if _, err = s.Store.SaveGroup(group); err != nil {
				t.Fatal(err)
			}
			if w := responseRequest(s, key.Key, "/v1/search/messages", request); w.Code != 403 {
				t.Fatalf("group bypass %d %s", w.Code, w.Body.String())
			}
			group.ModelIDs = []string{target}
			if _, err = s.Store.SaveGroup(group); err != nil {
				t.Fatal(err)
			}
			s.Store.UpdateSettings(func(st *storage.Settings) {
				if name == provider.TeleAgent {
					st.TeleAgent.SearchEnabled = false
				} else {
					st.MobileClaw.SearchEnabled = false
				}
			})
			if w := responseRequest(s, key.Key, "/v1/search/messages", request); w.Code != 403 {
				t.Fatal("disabled search accepted")
			}
			if calls != before {
				t.Fatal("denied request reached upstream")
			}
			s.Store.UpdateSettings(func(st *storage.Settings) {
				if name == provider.TeleAgent {
					st.TeleAgent.SearchEnabled = true
				} else {
					st.MobileClaw.SearchEnabled = true
				}
			})
			// Application errors must not be reported as empty successful searches.
			body = `{"code":123,"errno":123,"message":"secret upstream detail"}`
			if w := responseRequest(s, key.Key, "/v1/search/messages", request); w.Code != 502 || strings.Contains(w.Body.String(), "secret upstream detail") {
				t.Fatalf("business error %d %s", w.Code, w.Body.String())
			}
		})
	}
}
