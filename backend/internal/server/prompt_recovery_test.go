package server

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"codearts2api/internal/provider"
	"codearts2api/internal/requestpolicy"
	"codearts2api/internal/store"
)

// A prompt recovery must retain the original lease but recheck current access
// before making a second request. Native clients and HTTP handlers both run.
func TestPromptRecoveryRetainsLeaseAndRechecksAccess(t *testing.T) {
	for _, platform := range []string{provider.CodeArts, provider.WorkBuddy, provider.Trae} {
		for _, stream := range []bool{false, true} {
			for _, revoke := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%v/revoke=%v", platform, stream, revoke), func(t *testing.T) {
					var s *Server
					var group store.AccountGroup
					var account string
					var calls atomic.Int32
					var firstAuth string
					s, key, model, account := interruptionServer(t, platform, func(w http.ResponseWriter, r *http.Request) {
						n := calls.Add(1)
						if got := s.Store.PoolSnapshot()[account].InFlight; got != 1 {
							t.Errorf("prompt recovery lost original lease: %d", got)
						}
						auth := r.Header.Get("Authorization")
						if platform == provider.CodeArts {
							auth = strings.SplitN(auth, ",", 2)[0]
						}
						raw, _ := io.ReadAll(r.Body)
						if n == 1 {
							firstAuth = auth
							if revoke {
								group.ModelIDs = nil
								if _, err := s.Store.SaveGroup(group); err != nil {
									t.Error(err)
								}
							}
							w.WriteHeader(400)
							if platform == provider.WorkBuddy {
								io.WriteString(w, `{"code":11128,"msg":"blocked by security policy"}`)
							} else {
								io.WriteString(w, `{"code":"content_policy_violation","message":"blocked by security policy"}`)
							}
							return
						}
						if n != 2 || auth != firstAuth || !strings.Contains(string(raw), requestpolicy.FallbackSystemPrompt) {
							t.Error("recovery changed credentials, prompt or attempt count")
						}
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, interruptionPayload(platform, "POLICY_RECOVERED", true))
					})
					s.Store.UpdateSettings(func(settings *store.Settings) {
						p := settings.ProviderPolicy(platform)
						p.PromptMode = "passthrough"
						p.Sanitize = false
						p.PromptFallback = true
						switch platform {
						case provider.CodeArts:
							settings.CodeArts = p
						case provider.WorkBuddy:
							settings.WorkBuddy = p
						case provider.Trae:
							settings.Trae = p
						}
					})
					var err error
					group, err = s.Store.SaveGroup(store.AccountGroup{Name: "prompt recovery", AccountIDs: []string{account}, AllowlistEnabled: true, ModelIDs: []string{model}})
					if err != nil {
						t.Fatal(err)
					}
					if err = s.Store.SetKeyGroupAndSave(key, group.ID); err != nil {
						t.Fatal(err)
					}
					body := fmt.Sprintf(`{"model":%q,"stream":%v,"messages":[{"role":"system","content":"CLIENT"},{"role":"user","content":"hi"}]}`, model, stream)
					response := responseRequest(s, key, "/v1/chat/completions", body)
					wantCalls, wantStatus := int32(2), 200
					if revoke {
						wantCalls, wantStatus = 1, 403
					}
					if calls.Load() != wantCalls || response.Code != wantStatus {
						t.Fatalf("wrong recovery boundary: calls=%d status=%d body=%s", calls.Load(), response.Code, response.Body.String())
					}
					if s.Store.PoolSnapshot()[account].InFlight != 0 || s.Store.Account(account).Failures != 0 || s.Store.Account(account).CooldownUntil != 0 {
						t.Fatal("recovery leaked lease or penalized healthy account")
					}
					log := s.Store.Logs(1)[0]
					if log.Attempts != int(wantCalls) || len(log.AttemptDetails) != int(wantCalls) || log.AttemptDetails[0].Status != 400 || log.AttemptDetails[0].ErrorKind != "content_blocked" {
						t.Fatal("blocked attempt accounting changed")
					}
					if !revoke && (!log.Usage.Known || log.Usage.InputTokens <= 0 || log.Usage.OutputTokens <= 0) {
						t.Fatal("recovery lost measured usage")
					}
				})
			}
		}
	}
}
