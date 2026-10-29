package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/store"
)

// Both the client-facing request and the controlled upstream use real HTTP.
// No real credentials or native model services are involved.
func TestMessagesThinkingAtProviderBoundary(t *testing.T) {
	for _, name := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct{ name, fields, errorField string }{
				{"omitted", "", ""},
				{"null", `,"thinking":null`, ""},
				{"disabled", `,"thinking":{"type":"disabled"}`, ""},
				{"enabled", `,"thinking":{"type":"enabled"}`, "thinking.type"},
				{"adaptive_cli", `,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}`, "output_config.effort"},
				{"budget_cli", `,"thinking":{"type":"enabled","budget_tokens":4096}`, "budget_tokens"},
				{"effort", `,"output_config":{"effort":"low"}`, "output_config.effort"},
			} {
				t.Run(fmt.Sprintf("%s/stream=%v/%s", name, stream, tc.name), func(t *testing.T) {
					var calls atomic.Int32
					upstream := func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						var body map[string]json.RawMessage
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						if tc.name == "disabled" {
							var thinking struct{ Type string }
							if err := json.Unmarshal(body["thinking"], &thinking); err != nil || thinking.Type != "disabled" {
								t.Errorf("explicit disable lost before native HTTP: %s", body["thinking"])
							}
						} else if _, exists := body["thinking"]; exists {
							t.Error("thinking injected into an unchanged default request")
						}
						if _, exists := body["reasoning_effort"]; exists {
							t.Error("unsupported effort guessed or conflicting effort sent")
						}
						if name == provider.Trae {
							w.Header().Set("Content-Type", "text/event-stream")
							io.WriteString(w, traeTextFixture)
						} else {
							var nativeStream bool
							_ = json.Unmarshal(body["stream"], &nativeStream)
							if nativeStream {
								w.Header().Set("Content-Type", "text/event-stream")
								io.WriteString(w, summaryFixture("THINKING_CONTROL_OK", "stop", 10, 2))
							} else {
								w.Header().Set("Content-Type", "application/json")
								io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"THINKING_CONTROL_OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
							}
						}
						// A disabled native switch does not filter the returned answer.
					}
					var s *Server
					var key, model, account string
					switch name {
					case provider.WorkBuddy:
						s, key, _ = nativeWBServer(t, upstream)
						addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
						model, account = "workbuddy/cn/shared", "wb-cn"
					case provider.Trae:
						s, key, _ = nativeTraeServer(t, upstream)
						addTrae(t, s)
						model, account = "trae/cn/shared", "trae-acct"
					default:
						origin := httptest.NewServer(http.HandlerFunc(upstream))
						defer origin.Close()
						target, _ := url.Parse(origin.URL)
						transport := http.DefaultTransport.(*http.Transport).Clone()
						transport.Proxy = nil
						defer transport.CloseIdleConnections()
						s, key = responseTestServer(t, func(r *http.Request) (*http.Response, error) {
							cloned := r.Clone(r.Context())
							cloned.URL.Scheme, cloned.URL.Host = target.Scheme, target.Host
							cloned.Host = target.Host
							return transport.RoundTrip(cloned)
						})
						model, account = "test-model", "a"
					}
					s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 3 })
					before := s.Store.Account(account)
					gateway := httptest.NewServer(s.Handler())
					defer gateway.Close()
					body := fmt.Sprintf(`{"model":%q,"stream":%v,"max_tokens":64,"messages":[{"role":"user","content":"hello 11128"}]%s}`, model, stream, tc.fields)
					req, _ := http.NewRequest("POST", gateway.URL+"/v1/messages", strings.NewReader(body))
					req.Header.Set("Authorization", "Bearer "+key)
					req.Header.Set("Content-Type", "application/json")
					client := gateway.Client()
					client.Timeout = 3 * time.Second
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					raw, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					wantStatus, wantCalls, errorField := 200, int32(1), tc.errorField
					if errorField != "" || name == provider.Trae && tc.name == "disabled" {
						wantStatus, wantCalls = 400, 0
						if errorField == "" {
							errorField = "Trae"
						}
						if !strings.Contains(string(raw), "invalid_request_error") || !strings.Contains(string(raw), errorField) {
							t.Errorf("missing actionable input error: %s", raw)
						}
					}
					if resp.StatusCode != wantStatus || calls.Load() != wantCalls {
						t.Fatalf("status=%d calls=%d want=%d/%d body=%s", resp.StatusCode, calls.Load(), wantStatus, wantCalls, raw)
					}
					after := s.Store.Account(account)
					wantHealth := before.Status
					if wantCalls == 1 {
						wantHealth = "ok"
					}
					if after.Failures != before.Failures || after.CooldownUntil != before.CooldownUntil || after.Status != wantHealth || s.Store.PoolSnapshot()[account].InFlight != 0 {
						t.Fatalf("thinking preference damaged account health or leaked lease: status=%s failures=%d cooldown=%d", after.Status, after.Failures, after.CooldownUntil)
					}
				})
			}
		}
	}
}

func TestMessagesThinkingLeavesOtherTraeProtocolsUnchanged(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			for _, thinkingType := range []string{"disabled", "enabled", "adaptive"} {
				t.Run(fmt.Sprintf("%s/stream=%v/%s", path, stream, thinkingType), func(t *testing.T) {
					var calls atomic.Int32
					s, key, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						if body["reasoning_effort"] != "high" {
							t.Error("existing reasoning_effort mapping changed")
						}
						if path == "/v1/chat/completions" {
							thinking, _ := body["thinking"].(map[string]any)
							if thinking["type"] != thinkingType {
								t.Error("existing Chat passthrough changed")
							}
						} else if _, exists := body["thinking"]; exists {
							t.Error("Responses gained a previously ignored nonstandard field")
						}
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, traeTextFixture)
					})
					addTrae(t, s)
					body := map[string]any{"model": "trae/cn/shared", "stream": stream, "thinking": map[string]any{"type": thinkingType}}
					if path == "/v1/chat/completions" {
						body["messages"] = []map[string]string{{"role": "user", "content": "hello"}}
						body["reasoning_effort"] = "high"
					} else {
						body["input"] = "hello"
						body["reasoning"] = map[string]string{"effort": "high"}
					}
					gateway := httptest.NewServer(s.Handler())
					defer gateway.Close()
					req, _ := http.NewRequest("POST", gateway.URL+path, strings.NewReader(string(mustJSON(body))))
					req.Header.Set("Authorization", "Bearer "+key)
					req.Header.Set("Content-Type", "application/json")
					client := gateway.Client()
					client.Timeout = 3 * time.Second
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					raw, err := io.ReadAll(resp.Body)
					if err != nil || resp.StatusCode != 200 || calls.Load() != 1 {
						t.Fatalf("other protocol changed: status=%d calls=%d err=%v body=%s", resp.StatusCode, calls.Load(), err, raw)
					}
				})
			}
		}
	}
}
