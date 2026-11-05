package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func TestRawSummaryReplacementRechecksLivePolicy(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			// ForceModel + unified definitions is now rejected at configuration
			// commit (covered by storage's policy-conflict tests).
			for _, mutation := range []string{"allowlist", "accounts", "cooldown", "catalog", "budget"} {
				t.Run(fmt.Sprintf("%s/%v/%s", path, stream, mutation), func(t *testing.T) {
					calls := 0
					var change func()
					s, key, group := mappedFixture(t, func(_ string, w http.ResponseWriter, r *http.Request) {
						calls++
						if calls > 1 {
							t.Error("unauthorized replacement reached upstream")
						}
						change()
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, summaryFixture("PRIVATE_PARTIAL", "length", 10, 4))
					})
					onlyMappedPlatform(s, provider.CodeArts)
					s.Reg.ReplaceProvider(provider.CodeArts, "", []provider.Model{{ID: "test-model"}, {ID: "backup-model"}})
					policy := storage.DefaultModelPolicies()["deepseek-v4.1-flash"]
					policy.SummaryFallbackModel = "backup-model"
					if err := s.Store.SetModelPolicyAndSave("test-model", policy); err != nil {
						t.Fatal(err)
					}
					group.ModelIDs = append(group.ModelIDs, "backup-model")
					if _, err := s.Store.SaveGroup(group); err != nil {
						t.Fatal(err)
					}
					if mutation == "budget" {
						if err := s.Store.SetKeyBudgetAndSave(key, storage.KeyBudget{MaxTokens: 14}, false); err != nil {
							t.Fatal(err)
						}
					}
					status := 403
					change = func() {
						switch mutation {
						case "allowlist":
							group.ModelIDs = mappedSources
						case "accounts":
							group.AccountIDs = []string{"wb-cn", "trae-acct"}
							group.ModelMappings[0].Sources = []string{"workbuddy/cn/shared", "trae/cn/shared"}
						case "cooldown":
							status = 422
							s.Store.MarkModelResult("a", "backup-model", false, "fixture", 60)
							return
						case "catalog":
							status = 422
							s.Reg.ReplaceProvider(provider.CodeArts, "", []provider.Model{{ID: "test-model"}})
							return
						case "budget":
							status = 402
							return
						}
						if _, err := s.Store.SaveGroup(group); err != nil {
							t.Error(err)
						}
					}
					r := httptest.NewRequest("POST", path, strings.NewReader(mappedBody(path, stream, "codearts/test-model")))
					r.Header.Set("Authorization", "Bearer "+key)
					r.Header.Set("X-CodeArts-Request-Kind", "summary")
					w := httptest.NewRecorder()
					s.Handler().ServeHTTP(w, r)
					if calls != 1 || w.Code != status || strings.Contains(w.Body.String(), "PRIVATE_PARTIAL") {
						t.Fatalf("replacement bypassed current policy: calls=%d status=%d want=%d body=%s", calls, w.Code, status, w.Body.String())
					}
					entry := s.Store.Logs(1)[0]
					if entry.SummaryAttempts != 1 || len(entry.AttemptDetails) != 1 || entry.Usage.InputTokens+entry.Usage.OutputTokens != 14 || s.Store.Account("a").Failures != 0 {
						t.Fatal("rejected replacement changed usage or account health", entry)
					}
					for _, pool := range s.Store.PoolSnapshot() {
						if pool.InFlight != 0 {
							t.Fatal("replacement leaked a lease")
						}
					}
				})
			}
		}
	}
}
