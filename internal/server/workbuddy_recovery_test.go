package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
	"codearts2api/internal/workbuddy"
)

func recoveryBody(path string, stream bool) string {
	if path == "/v1/responses" {
		return fmt.Sprintf(`{"model":"workbuddy/cn/shared","input":"hi","stream":%v,"max_output_tokens":64}`, stream)
	}
	return fmt.Sprintf(`{"model":"workbuddy/cn/shared","messages":[{"role":"user","content":"hi"}],"stream":%v,"max_tokens":64}`, stream)
}

// Real HTTP on both hops, including the native WorkBuddy client. Only the
// upstream server and credentials are fixtures; the gateway is unchanged.
func TestWorkBuddyRecoveryAcrossProtocols(t *testing.T) {
	tool := "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"id\\\":9007199254740993}\"}}]}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n"
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			for _, tools := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%v/tools=%v", path, stream, tools), func(t *testing.T) {
					var calls atomic.Int32
					var firstBody []byte
					var rootID string
					s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
						body, _ := io.ReadAll(r.Body)
						if calls.Add(1) == 1 {
							firstBody = body
							rootID = r.Header.Get("X-Root-Request-ID")
							w.WriteHeader(500)
							io.WriteString(w, `{"code":10000,"msg":"private upstream detail"}`)
							return
						}
						if string(body) != string(firstBody) {
							t.Error("recovery changed the native request body")
						}
						if rootID == "" || r.Header.Get("X-Root-Request-ID") != rootID {
							t.Error("recovery lost request correlation")
						}
						w.Header().Set("Content-Type", "text/event-stream")
						if tools {
							io.WriteString(w, tool)
						} else {
							io.WriteString(w, summaryFixture("RECOVERED_EXACT", "stop", 10, 4))
						}
					})
					addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
					s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 3 })
					gateway := httptest.NewServer(s.Handler())
					defer gateway.Close()
					req, _ := http.NewRequest("POST", gateway.URL+path, strings.NewReader(recoveryBody(path, stream)))
					req.Header.Set("Authorization", "Bearer "+key)
					client := gateway.Client()
					client.Timeout = 5 * time.Second
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					raw, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					want := "RECOVERED_EXACT"
					if tools {
						want = "9007199254740993"
					}
					if resp.StatusCode != 200 || calls.Load() != 2 || !strings.Contains(string(raw), want) || strings.Contains(string(raw), "private upstream detail") {
						t.Fatalf("status=%d calls=%d body=%s", resp.StatusCode, calls.Load(), raw)
					}
					log := s.Store.Logs(1)[0]
					if log.Attempts != 2 || len(log.AttemptDetails) != 2 || log.AttemptDetails[0].Status != 502 || log.AttemptDetails[1].Status != 200 || !log.UsageIncomplete || log.Usage.InputTokens != 10 || log.Usage.OutputTokens != 4 {
						t.Fatalf("recovery accounting lost an attempt: %+v", log)
					}
					if log.AttemptDetails[0].DurationMS == nil || log.AttemptDetails[1].DurationMS == nil || log.AttemptDetails[0].FirstTokenMS != nil {
						t.Fatal("failure/recovery timing missing or mixed")
					}
					if log.AttemptDetails[0].UpstreamStatus != 500 || log.AttemptDetails[1].UpstreamStatus != 0 {
						t.Fatal("upstream failure status lost or copied into successful attempt")
					}
					if a := s.Store.Account("wb-cn"); a.Failures != 0 || a.CooldownUntil != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
						t.Fatal("recovered request damaged credentials or leaked its lease")
					}
					if h := s.Store.ModelHealthSnapshot()["wb-cn"]["workbuddy/cn/shared"]; h.Failures != 0 || h.CooldownUntil != 0 {
						t.Fatal("successful recovery left model cooling")
					}
					if u := s.Store.Keys()[0].BudgetUsage; u.Requests != 1 || u.Tokens != 14 {
						t.Fatalf("recovery charged multiple client requests: %+v", u)
					}
				})
			}
		}
	}
}

func noRecoveryWait(t *testing.T) {
	t.Helper()
	old := waitRetry
	waitRetry = func(ctx context.Context, d time.Duration) error {
		if d != workBuddyRetryWait {
			t.Errorf("unexpected recovery wait %v", d)
		}
		return ctx.Err()
	}
	t.Cleanup(func() { waitRetry = old })
}

func TestWorkBuddyPersistent5xxUsesShortModelCooldown(t *testing.T) {
	noRecoveryWait(t)
	for _, status := range []int{500, 502, 503, 504} {
		for _, maxAttempts := range []int{1, 2, 3} {
			t.Run(fmt.Sprintf("%d/attempts=%d", status, maxAttempts), func(t *testing.T) {
				calls := 0
				s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.WriteHeader(status)
					io.WriteString(w, `{"code":10000}`)
				})
				addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
				s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = maxAttempts; st.Pool.RateCooldownSec = 60 })
				w := responseRequest(s, key, "/v1/chat/completions", recoveryBody("/v1/chat/completions", false))
				if w.Code != 502 || calls != min(maxAttempts, 2) {
					t.Fatalf("persistent failure status=%d calls=%d", w.Code, calls)
				}
				h := s.Store.ModelHealthSnapshot()["wb-cn"]["workbuddy/cn/shared"]
				if remaining := h.CooldownUntil - time.Now().Unix(); h.Failures != 1 || remaining <= 0 || remaining > 5 {
					t.Fatalf("ordinary 5xx has wrong cooldown: %+v", h)
				}
				if a := s.Store.Account("wb-cn"); a.Failures != 0 || a.CooldownUntil != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
					t.Fatal("ordinary 5xx penalized credentials or leaked a lease")
				}
				before := calls
				w = responseRequest(s, key, "/v1/chat/completions", recoveryBody("/v1/chat/completions", false))
				if calls != before || w.Code != 503 {
					t.Fatal("a new request bypassed model cooling")
				}
			})
		}
	}
}

func TestWorkBuddyRecoverySharesTotalAttemptLimitWithFailover(t *testing.T) {
	noRecoveryWait(t)
	for _, limit := range []int{2, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			var auth []string
			s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
				auth = append(auth, r.Header.Get("Authorization"))
				if len(auth) <= 2 {
					w.WriteHeader(500)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, summaryFixture("BACKUP_OK", "stop", 10, 4))
			})
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			raw, _ := json.Marshal(workbuddy.Credentials{AccessToken: "backup-access", RefreshToken: "backup-refresh", UID: "backup", Realm: "cn", ExpiresAt: time.Now().Add(time.Hour).Unix()})
			_, err := s.Store.UpsertAccountAndSave(&codearts.Account{ID: "backup", Provider: provider.WorkBuddy, Scope: "cn", ExternalID: "backup", Credentials: raw, Enabled: true}, false)
			if err != nil {
				t.Fatal(err)
			}
			s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = limit })
			w := responseRequest(s, key, "/v1/chat/completions", recoveryBody("/v1/chat/completions", false))
			if len(auth) != limit || auth[0] != auth[1] || limit == 3 && (auth[1] == auth[2] || w.Code != 200 || !strings.Contains(w.Body.String(), "BACKUP_OK")) || limit == 2 && w.Code != 502 {
				t.Fatalf("retry/failover exceeded or lost slots: limit=%d calls=%d status=%d", limit, len(auth), w.Code)
			}
			if s.Store.Logs(1)[0].Attempts != limit {
				t.Fatal("actual attempt count differs from log")
			}
			for _, p := range s.Store.PoolSnapshot() {
				if p.InFlight != 0 {
					t.Fatal("failover leaked a lease")
				}
			}
		})
	}
}

func TestWorkBuddyRecoveryNeverReplaysObservedFrames(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			for name, frame := range map[string]string{
				"text":  `{"choices":[{"index":0,"delta":{"content":"PARTIAL_EXACT"}}]}`,
				"role":  `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
				"usage": `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":1}}`,
			} {
				t.Run(fmt.Sprintf("%s/%v/%s", path, stream, name), func(t *testing.T) {
					calls := 0
					s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
						calls++
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: %s\n\ndata: {\"code\":6021}\n\n", frame)
					})
					addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
					s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 3 })
					w := responseRequest(s, key, path, recoveryBody(path, stream))
					if calls != 1 || s.Store.Logs(1)[0].Status != 502 {
						t.Fatalf("partial upstream reply replayed/hidden: calls=%d status=%d body=%s", calls, w.Code, w.Body.String())
					}
					if stream && name == "text" && strings.Count(w.Body.String(), "PARTIAL_EXACT") != 1 {
						t.Fatal("partial streamed text lost or duplicated")
					}
				})
			}
		}
	}
}

func TestWorkBuddyRecoveryCancellationAndBudget(t *testing.T) {
	for _, cause := range []string{"cancel", "token_budget"} {
		t.Run(cause, func(t *testing.T) {
			calls := 0
			s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 3 })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			old := waitRetry
			t.Cleanup(func() { waitRetry = old })
			waitRetry = func(ctx context.Context, d time.Duration) error {
				if cause == "cancel" {
					cancel()
					return old(ctx, d)
				}
				return nil
			}
			if cause == "token_budget" {
				if err := s.Store.SetKeyBudgetAndSave(key, store.KeyBudget{MaxTokens: 64}, false); err != nil {
					t.Fatal(err)
				}
			}
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(recoveryBody("/v1/chat/completions", false))).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if calls != 1 || cause == "token_budget" && w.Code != 402 {
				t.Fatalf("canceled/over-budget recovery reached upstream: calls=%d status=%d", calls, w.Code)
			}
			if a := s.Store.Account("wb-cn"); a.Failures != 0 || a.CooldownUntil != 0 || s.Store.PoolSnapshot()[a.ID].InFlight != 0 {
				t.Fatal("stopped recovery damaged credentials or leaked lease")
			}
		})
	}
}

func TestWorkBuddyRecoveryEligibility(t *testing.T) {
	for _, name := range []string{"allowed", "slow", "no_context", "not_configured", "already_retried", "observed", "committed", "trae", "auth", "blocked", "429", "404", "501", "transport", "canceled"} {
		t.Run(name, func(t *testing.T) {
			ctx := withUsageCapture(context.Background(), "chat")
			configureInference(ctx, time.Now(), true)
			resetUsage(ctx)
			c := ctx.Value(usageContextKey{}).(*usageCapture)
			acct := &codearts.Account{Provider: provider.WorkBuddy}
			var err error = &provider.Error{Status: 500}
			switch name {
			case "slow":
				c.started = time.Now().Add(-workBuddyRetryWindow)
			case "no_context":
				ctx = context.Background()
			case "not_configured":
				c.started = time.Time{}
			case "already_retried":
				resetUsage(ctx)
			case "observed":
				captureUsage(ctx, []byte(`{"choices":[]}`))
			case "committed":
				err = stopAfterOutput(err, true)
			case "trae":
				acct.Provider = provider.Trae
			case "auth":
				err = &provider.Error{Status: 500, AccountFault: true}
			case "blocked":
				err = &provider.Error{Status: 500, ContentBlocked: true}
			case "429":
				err = &provider.Error{Status: 429}
			case "404":
				err = &provider.Error{Status: 404}
			case "501":
				err = &provider.Error{Status: 501}
			case "transport":
				err = provider.NewTransportError("WorkBuddy", errors.New("network"))
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if got := canRecoverWorkBuddy(ctx, acct, err); got != (name == "allowed") {
				t.Fatalf("eligibility=%v", got)
			}
		})
	}
}

func TestWorkBuddyRecoveryWindowOnlyAdmitsRetry(t *testing.T) {
	s, _, _ := nativeWBServer(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP request") })
	acct := &codearts.Account{Provider: provider.WorkBuddy}
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			ctx := withUsageCapture(context.Background(), "chat")
			configureInference(ctx, time.Now(), true)
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			old := waitRetry
			t.Cleanup(func() { waitRetry = old })
			waitRetry = func(context.Context, time.Duration) error {
				if expired {
					ctx.Value(usageContextKey{}).(*usageCapture).started = time.Now().Add(-workBuddyRetryWindow)
				}
				return nil
			}
			calls := 0
			err, repeated := s.runAccountAttempt(ctx, acct, "workbuddy/cn/shared", func(ctx context.Context, acct *codearts.Account) error {
				calls++
				startAttempt(ctx, acct, "workbuddy/cn/shared")
				if calls == 1 {
					return &provider.Error{Status: 500}
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) < 20*time.Second {
					t.Fatal("retry window became a generation timeout")
				}
				return nil
			}, true)
			if expired && (calls != 1 || repeated || err == nil) || !expired && (calls != 2 || !repeated || err != nil) {
				t.Fatalf("expired=%v calls=%d repeated=%v err=%v", expired, calls, repeated, err)
			}
		})
	}
}

func TestWorkBuddyRecoveryRetainsLeaseDuringWait(t *testing.T) {
	calls := 0
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, summaryFixture("LEASE_OK", "stop", 10, 4))
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	s.Store.UpdateSettings(func(st *store.Settings) { st.MaxRetries = 3; st.Pool.MaxInFlight = 1 })
	old := waitRetry
	t.Cleanup(func() { waitRetry = old })
	waitRetry = func(context.Context, time.Duration) error {
		if s.Store.PoolSnapshot()["wb-cn"].InFlight != 1 {
			t.Fatal("retry released its account lease before completion")
		}
		a, release := s.Store.AcquireAccountForModel("another-session", "workbuddy/cn/shared", nil)
		if a != nil {
			release()
			t.Fatal("another request bypassed the account concurrency limit")
		}
		return nil
	}
	w := responseRequest(s, key, "/v1/chat/completions", recoveryBody("/v1/chat/completions", false))
	if w.Code != 200 || calls != 2 || s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
		t.Fatal("recovery failed or leaked its lease")
	}
}

func TestWorkBuddyRecoveryLeavesOtherErrorCooldownsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		provider string
		status   int
		seconds  int64
	}{
		{provider.WorkBuddy, 429, 60},
		{provider.WorkBuddy, 404, 60},
		{provider.WorkBuddy, 501, 60},
		{provider.WorkBuddy, 401, 300},
		{provider.Trae, 500, 60},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.provider, tc.status), func(t *testing.T) {
			s, _, _ := nativeWBServer(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP request") })
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			s.Store.UpdateSettings(func(st *store.Settings) { st.Pool.RateCooldownSec = 60; st.Pool.BreakerCooldownSec = 300 })
			a := s.Store.Account("wb-cn")
			a.Provider = tc.provider
			s.providerAccountError(context.Background(), a, "model", &provider.Error{Status: tc.status, AccountFault: tc.status == 401})
			expires := s.Store.ModelHealthSnapshot()[a.ID]["model"].CooldownUntil
			if tc.status == 401 {
				expires = s.Store.Account(a.ID).CooldownUntil
			}
			if remaining := expires - time.Now().Unix(); remaining < tc.seconds-1 || remaining > tc.seconds {
				t.Fatalf("existing error cooldown changed: seconds=%d want=%d", remaining, tc.seconds)
			}
		})
	}
}
