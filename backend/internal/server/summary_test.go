package server

import (
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func summaryFixture(text, finish string, in, out int) string {
	chunk := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out}}
	raw, _ := json.Marshal(chunk)
	return "data: " + string(raw) + "\n\ndata: [DONE]\n\n"
}
func TestSummaryRetryAcrossProtocols(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "messages"} {
		for _, stream := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%v", protocol, stream), func(t *testing.T) {
				var calls atomic.Int32
				s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
					var body map[string]any
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Fatal("bad JSON")
					}
					if body["thinking"].(map[string]any)["type"] != "disabled" {
						t.Error("summary thinking not disabled")
					}
					if body["max_tokens"] != float64(512) {
						t.Error("caller token ceiling changed")
					}
					n := calls.Add(1)
					if n == 1 {
						return responseUpstream(r, summaryFixture("PRIVATE_PARTIAL_SHOULD_NOT_ESCAPE", "length", 10, 8)), nil
					}
					if !strings.Contains(string(mustJSON(body)), "under 900 words") {
						t.Error("retry did not change summary guidance")
					}
					return responseUpstream(r, summaryFixture("FINAL_COMPLETE_SUMMARY", "stop", 12, 6)), nil
				})
				s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}})
				body := map[string]any{"model": "deepseek-v4.1-flash", "stream": stream, "max_tokens": 512, "messages": []any{map[string]any{"role": "system", "content": "You are a context summarization assistant. Test."}, map[string]any{"role": "user", "content": "History"}}}
				path := "/v1/chat/completions"
				if protocol == "responses" {
					path = "/v1/responses"
					delete(body, "messages")
					delete(body, "max_tokens")
					body["max_output_tokens"] = 512
					body["instructions"] = "You are a context summarization assistant. Test."
					body["input"] = "History"
				}
				if protocol == "messages" {
					path = "/v1/messages"
					body["system"] = "You are a context summarization assistant. Test."
					body["messages"] = []any{map[string]any{"role": "user", "content": "History"}}
				}
				w := responseRequest(s, key, path, string(mustJSON(body)))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "FINAL_COMPLETE_SUMMARY") || strings.Contains(w.Body.String(), "PRIVATE_PARTIAL") {
					t.Fatalf("partial output escaped or final missing: %d %s", w.Code, w.Body.String())
				}
				if calls.Load() != 2 {
					t.Fatalf("attempts=%d", calls.Load())
				}
				log := s.Store.Logs(1)[0]
				if log.Usage.InputTokens != 22 || log.Usage.OutputTokens != 14 || log.SummaryAttempts != 2 || log.RequestKind != "summary" || log.FinishReason != "stop" || log.RequestID == "" {
					t.Fatalf("incomplete audit: %+v", log)
				}
				if w.Header().Get("X-Request-ID") != log.RequestID {
					t.Fatal("request correlation lost")
				}
				if s.Store.Stats().SummaryRetries != 1 {
					t.Fatal("retry metric missing")
				}
			})
		}
	}
}
func TestSummaryRepeatedTruncationDoesNotPoisonAccount(t *testing.T) {
	var calls atomic.Int32
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return responseUpstream(r, summaryFixture("PARTIAL", "length", 10, 8)), nil
	})
	s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}})
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"deepseek-v4.1-flash","stream":true,"messages":[{"role":"system","content":"You are a context summarization assistant."},{"role":"user","content":"History"}]}`)
	if w.Code != 422 || strings.Contains(w.Body.String(), "PARTIAL") || calls.Load() != 2 {
		t.Fatalf("unbounded retry / partial summary: %d %d %s", w.Code, calls.Load(), w.Body.String())
	}
	if s.Store.Account("a").Failures != 0 {
		t.Fatal("summary length marked account unhealthy")
	}
	failureLog := s.Store.Logs(1)[0]
	if failureLog.EstimatedCost == nil || failureLog.Usage.OutputTokens != 16 || len(failureLog.AttemptDetails) != 2 {
		t.Fatal("failed summary lost measured retry usage/cost")
	}
	if s.Store.Stats().SummaryFailures != 1 || s.Store.Stats().TruncatedRequests != 1 {
		t.Fatal("failure metrics missing")
	}
}
func TestExplicitSummaryAndDisabledPolicy(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			var calls atomic.Int32
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return responseUpstream(r, summaryFixture("PARTIAL", "length", 10, 8)), nil
			})
			s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}})
			p, _ := s.Store.ModelPolicy("deepseek-v4.1-flash")
			p.SummaryEnabled = enabled
			p.SummaryRetry = false
			if err := s.Store.SetModelPolicyAndSave("deepseek-v4.1-flash", p); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"deepseek-v4.1-flash","stream":true,"messages":[{"role":"user","content":"Summarize this history"}]}`))
			r.Header.Set("Authorization", "Bearer "+key)
			r.Header.Set("X-CodeArts-Request-Kind", "summary")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if enabled && w.Code != 422 || !enabled && w.Code != 200 || calls.Load() != 1 {
				t.Fatal("summary policy did not apply")
			}
		})
	}
}
func TestUnsupportedNativeCapabilityDoesNotFallThrough(t *testing.T) {
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("unsupported request reached upstream")
		return nil, nil
	})
	for _, path := range []string{"/v1/responses/compact", "/responses/compact"} {
		w := responseRequest(s, key, path, `{}`)
		if w.Code != 501 || !strings.Contains(w.Body.String(), "unsupported_capability") {
			t.Fatal("unsupported native compaction not explicit")
		}
	}
}

func TestMessagesLengthIsNotNormalEndTurn(t *testing.T) {
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		return responseUpstream(r, summaryFixture("cut off", "length", 10, 8)), nil
	})
	w := responseRequest(s, key, "/v1/messages", `{"model":"test-model","stream":true,"max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if !strings.Contains(w.Body.String(), `"stop_reason":"max_tokens"`) || strings.Contains(w.Body.String(), `"stop_reason":"end_turn"`) {
		t.Fatalf("truncation hidden: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"output_tokens":8`) {
		t.Fatal("character length used instead of usage")
	}
}
func TestSummaryMissingFinishIsFailureWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return responseUpstream(r, summaryFixture("PARTIAL", "", 10, 8)), nil
	})
	s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}})
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"deepseek-v4.1-flash","stream":true,"messages":[{"role":"system","content":"You are a context summarization assistant."},{"role":"user","content":"history"}]}`)
	if w.Code != 422 || calls.Load() != 1 || strings.Contains(w.Body.String(), "PARTIAL") {
		t.Fatal("incomplete stream accepted/retried")
	}
}
func TestModelPolicyAPIChangesClientProfile(t *testing.T) {
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("configuration called upstream")
		return nil, nil
	})
	s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}})
	p, _ := s.Store.ModelPolicy("deepseek-v4.1-flash")
	p.KeepRecentTokens = 8192
	r := httptest.NewRequest("POST", "/admin/api/model-policies", strings.NewReader(string(mustJSON(map[string]any{"model": "deepseek-v4.1-flash", "policy": p}))))
	w := httptest.NewRecorder()
	s.handleAdminModelPolicies(w, r)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/v1/client-profile?model=deepseek-v4.1-flash", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"keep_recent_tokens":8192`) {
		t.Fatalf("profile stale: %s", w.Body.String())
	}
}

func TestOrdinaryStreamsDoNotHidePrematureEOF(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return responseUpstream(r, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"), nil
			})
			w := responseRequest(s, key, path, `{"model":"test-model","stream":true,"max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
			if !strings.Contains(w.Body.String(), "error") || calls.Load() != 1 {
				t.Fatalf("partial stream reported success or replayed: %s", w.Body.String())
			}
			if path == "/v1/messages" && strings.Contains(w.Body.String(), "message_stop") {
				t.Fatal("incomplete Anthropic stream ended normally")
			}
		})
	}
}

func TestSummaryFallbackModelAndPerAttemptPrices(t *testing.T) {
	var models []string
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("bad body")
		}
		models = append(models, body["model"].(string))
		if len(models) == 1 {
			return responseUpstream(r, summaryFixture("PRIVATE", "length", 1000000, 100000)), nil
		}
		if _, ok := body["thinking"]; ok {
			t.Fatal("unsupported thinking flag copied to unverified fallback")
		}
		return responseUpstream(r, summaryFixture("complete", "stop", 2000000, 100000)), nil
	})
	s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}, {ID: "backup-model"}})
	p, _ := s.Store.ModelPolicy("deepseek-v4.1-flash")
	p.SummaryFallbackModel = "backup-model"
	if err := s.Store.SetModelPolicyAndSave("deepseek-v4.1-flash", p); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetPriceAndSave("backup-model", store.ModelPrice{Currency: "USD", Input: 2, Output: 4, Cached: 1}); err != nil {
		t.Fatal(err)
	}
	w := responseRequest(s, key, "/v1/chat/completions", `{"model":"deepseek-v4.1-flash","messages":[{"role":"system","content":"You are a context summarization assistant."},{"role":"user","content":"history"}]}`)
	if w.Code != 200 || len(models) != 2 || models[1] != "backup-model" || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatalf("fallback failed %d %s", w.Code, w.Body.String())
	}
	log := s.Store.Logs(1)[0]
	if len(log.AttemptDetails) != 2 || log.AttemptDetails[1].Model != "backup-model" {
		t.Fatal("actual model not recorded")
	}
	// 1M*.30 + .1M*1.20 + 2M*2 + .1M*4 = $4.82.
	if log.EstimatedCost == nil || math.Abs(log.EstimatedCost.Amount-4.82) > 1e-9 {
		t.Fatalf("wrong cross-model billing: %+v", log.EstimatedCost)
	}
	if math.Abs(s.Store.Keys()[0].BudgetUsage.USD-4.82) > 1e-9 {
		t.Fatal("budget lost fallback spend")
	}
}
