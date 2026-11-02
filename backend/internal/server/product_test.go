package server

import (
	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/requestpolicy"
	"codearts2api/internal/storage"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func productBool(v bool) *bool { return &v }
func TestProductGroupDefaultAllProtocolsAndHiddenDirectCall(t *testing.T) {
	for _, platform := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", platform, path, stream), func(t *testing.T) {
					s, key, g := mappedFixture(t, nil)
					onlyMappedPlatform(s, platform)
					g.DefaultModel = mappedPublic
					g.ModelRules = []access.ModelRule{{ID: mappedPublic, Visible: productBool(false)}}
					if _, err := s.Store.SaveGroup(g); err != nil {
						t.Fatal(err)
					}
					if ids := modelIDsFromResponse(t, scopedModelRequest(s, key)); len(ids) != 0 {
						t.Fatal("hidden leaked", ids)
					}
					body := mappedBody(path, stream, mappedPublic)
					var raw map[string]json.RawMessage
					_ = json.Unmarshal([]byte(body), &raw)
					delete(raw, "model")
					w := responseRequest(s, key, path, string(mustJSON(raw)))
					if w.Code != 200 || !strings.Contains(w.Body.String(), mappedPublic) {
						t.Fatal(w.Code, w.Body.String())
					}
					if l := s.Store.Logs(1)[0]; l.RequestedModel != "" || l.Provider != platform || l.Model == mappedPublic || !l.Usage.Known {
						t.Fatal("identity/usage", l)
					}
					w = responseRequest(s, key, path, mappedBody(path, false, "misspelled-4.1"))
					if w.Code != 404 {
						t.Fatal("unknown rescued by default", w.Code, w.Body.String())
					}
					w = responseRequest(s, key, path, mappedBody(path, false, mappedPublic))
					if w.Code != 200 {
						t.Fatal("hidden can't direct-call", w.Code, w.Body.String())
					}
				})
			}
		}
	}
}
func TestProductPresetsPartialGroupUpdateAndClientPreference(t *testing.T) {
	s, key, g := mappedFixture(t, nil)
	g.DefaultModel = mappedPublic
	g.ClientDefaultModel = mappedPublic
	g.ModelRules = []access.ModelRule{{ID: "workbuddy/cn/shared", Visible: productBool(false)}}
	if _, err := s.Store.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetModelPolicyAndSave("test-model", storage.DefaultModelPolicies()["deepseek-v4.1-flash"]); err != nil {
		t.Fatal(err)
	}
	original := s.Store.Groups()
	w := adminProbeRequest(t, s, "GET", "/admin/api/groups/presets", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":1`) || !reflect.DeepEqual(original, s.Store.Groups()) {
		t.Fatal(w.Code, w.Body.String())
	}
	body := map[string]any{"id": g.ID, "name": "updated", "account_ids": g.AccountIDs, "model_ids": g.ModelIDs, "allowlist_enabled": g.AllowlistEnabled}
	w = adminProbeRequest(t, s, "POST", "/admin/api/groups", string(mustJSON(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved := s.Store.Groups()[0]
	if saved.DefaultModel != mappedPublic || saved.ClientDefaultModel != mappedPublic || !reflect.DeepEqual(saved.ModelRules, g.ModelRules) {
		t.Fatal(saved)
	}
	// Client preference is advisory. Inference still follows its independent default.
	req := scopedModelRequest(s, key)
	var list struct {
		Default string `json:"default_model"`
	}
	_ = json.Unmarshal(req.Body.Bytes(), &list)
	if list.Default != mappedPublic {
		t.Fatal(req.Body.String())
	}
	reqProfile := httptest.NewRequest("GET", "/v1/client-profile", nil)
	reqProfile.Header.Set("Authorization", "Bearer "+key)
	profile := httptest.NewRecorder()
	s.Handler().ServeHTTP(profile, reqProfile)
	if profile.Code != 200 || !strings.Contains(profile.Body.String(), `"model":"`+mappedPublic+`"`) {
		t.Fatal(profile.Code, profile.Body.String())
	}
	body["default_model"] = ""
	body["client_default_model"] = ""
	body["model_rules"] = []any{}
	w = adminProbeRequest(t, s, "POST", "/admin/api/groups", string(mustJSON(body)))
	if w.Code != 200 || len(s.Store.Groups()[0].ModelRules) != 0 || s.Store.Groups()[0].DefaultModel != "" {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestProductInheritedPromptAcrossProtocols(t *testing.T) {
	for _, platform := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
			t.Run(platform+path, func(t *testing.T) {
				calls := 0
				s, key, model, _ := interruptionServer(t, platform, func(w http.ResponseWriter, r *http.Request) {
					calls++
					raw, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(raw), "COMMON-POLICY") || strings.Contains(string(raw), "CLIENT-PROMPT") {
						t.Errorf("wrong inherited injection: %s", raw)
					}
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, interruptionPayload(platform, "INHERITED_OK", true))
				})
				s.Store.UpdateSettings(func(st *storage.Settings) {
					st.CommonPrompt = config.PromptSettings{Mode: "custom", Text: "COMMON-POLICY", Fallback: true, Sanitize: true}
					switch platform {
					case provider.CodeArts:
						st.CodeArts.PromptOverrides = &config.PromptOverrides{}
					case provider.WorkBuddy:
						st.WorkBuddy.PromptOverrides = &config.PromptOverrides{}
					case provider.Trae:
						st.Trae.PromptOverrides = &config.PromptOverrides{}
					}
				})
				w := responseRequest(s, key, path, mappedBody(path, true, model))
				if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "INHERITED_OK") {
					t.Fatal(calls, w.Code, w.Body.String())
				}
			})
		}
	}
}
func TestProductInheritedFallbackFalseAndAtomicSettingsValidation(t *testing.T) {
	for _, platform := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		t.Run(platform, func(t *testing.T) {
			calls := 0
			s, key, model, _ := interruptionServer(t, platform, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(400)
				if platform == provider.WorkBuddy {
					io.WriteString(w, `{"code":11128,"msg":"blocked by security policy"}`)
				} else {
					io.WriteString(w, `{"code":"content_policy_violation","message":"blocked by security policy"}`)
				}
			})
			patch := fmt.Sprintf(`{"common_prompt":{"prompt_mode":"passthrough","prompt_fallback":true},%q:{"prompt_overrides":{"prompt_fallback":false,"sanitize":false,"prompt_text":""}}}`, platform)
			postSettings(t, s, patch)
			w := responseRequest(s, key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, model))
			if calls != 1 || w.Code == 200 {
				t.Fatal("explicit false lost", calls, w.Code)
			}
			postSettings(t, s, fmt.Sprintf(`{%q:{"prompt_overrides":{}}}`, platform))
			effective := s.Store.Settings().ProviderPolicy(platform)
			if !effective.PromptFallback || !effective.Sanitize {
				t.Fatal("{} failed to clear old field overrides", effective)
			}
			old := s.Store.Settings()
			bad := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/admin/api/settings", strings.NewReader(`{"common_prompt":{"prompt_mode":"invalid"},"trae":{"prompt_text":"DO NOT SAVE"}}`))
			s.handleAdminSettings(bad, request)
			if bad.Code != 400 || !reflect.DeepEqual(old, s.Store.Settings()) {
				t.Fatal("invalid combined update partially applied", bad.Code)
			}
		})
	}
}

func TestProductInheritedRecoveryAndSummaryBoundaries(t *testing.T) {
	for _, platform := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		for _, summary := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/summary=%v", platform, summary), func(t *testing.T) {
				calls := 0
				s, key, model, _ := interruptionServer(t, platform, func(w http.ResponseWriter, r *http.Request) {
					calls++
					raw, _ := io.ReadAll(r.Body)
					if summary {
						if strings.Contains(string(raw), "COMMON MUST NOT APPEAR") {
							t.Error("common prompt replaced summary")
						}
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, interruptionPayload(platform, "SUMMARY_COMPLETE", true))
						return
					}
					if calls == 1 {
						if !strings.Contains(string(raw), "CLIENT-PROMPT") {
							t.Error("passthrough lost prompt")
						}
						w.WriteHeader(400)
						if platform == provider.WorkBuddy {
							io.WriteString(w, `{"code":11128,"msg":"blocked by security policy"}`)
						} else {
							io.WriteString(w, `{"code":"content_policy_violation","message":"blocked by security policy"}`)
						}
						return
					}
					if calls > 2 || !strings.Contains(string(raw), requestpolicy.FallbackSystemPrompt) {
						t.Error("inherited recovery exceeded bound or wrong prompt")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, interruptionPayload(platform, "RECOVERED", true))
				})
				mode := "passthrough"
				if summary {
					mode = "custom"
				}
				patch := fmt.Sprintf(`{"common_prompt":{"prompt_mode":%q,"prompt_text":"COMMON MUST NOT APPEAR","prompt_fallback":true,"sanitize":false},%q:{"prompt_overrides":{}}}`, mode, platform)
				postSettings(t, s, patch)
				body := mappedBody("/v1/chat/completions", true, model)
				req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+key)
				if summary {
					req.Header.Set("X-CodeArts-Request-Kind", "summary")
				}
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, req)
				want := 2
				if summary {
					want = 1
				}
				if w.Code != 200 || calls != want {
					t.Fatal(calls, w.Code, w.Body.String())
				}
			})
		}
	}
}
