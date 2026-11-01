package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/store"
	"codearts2api/internal/workbuddy"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The identical fixture is also compiled against the frozen M3.16 source.
// It compares wire results, actual work counts, checkpoints and charged usage.
func TestAcceptanceGrowthContract(t *testing.T) {
	type testCase struct {
		name, method, path, body string
		setup                    func(*Server, *growthActionFixture)
	}
	const base = "/admin/api/workbuddy/tasks"
	const paid = `{"id":"wb-cn","task_code":"Model_chat_GLM5.2"}`
	const activity = `{"id":"wb-cn","task_code":"chat_5"}`
	cases := []testCase{
		{"list", "GET", base + "?id=wb-cn", "", nil},
		{"list_missing_id", "GET", base, "", nil},
		{"list_missing_account", "GET", base + "?id=missing", "", nil},
		{"list_method", "POST", base, `{}`, nil},
		{"claim_method", "GET", base + "/claim", "", nil},
		{"claim_invalid_json", "POST", base + "/claim", `{`, nil},
		{"claim_extra_field", "POST", base + "/claim", `{"id":"wb-cn","task_code":"chat_5","extra":1}`, nil},
		{"claim_multiple_json", "POST", base + "/claim", activity + ` {}`, nil},
		{"claim_unknown", "POST", base + "/claim", `{"id":"wb-cn","task_code":"missing"}`, nil},
		{"claim_not_ready", "POST", base + "/claim", activity, nil},
		{"claim_ready", "POST", base + "/claim", activity, func(s *Server, f *growthActionFixture) { f.accepted["chat_5"] = true; f.progress["chat_5"] = 5 }},
		{"claim_already", "POST", base + "/claim", activity, func(s *Server, f *growthActionFixture) { f.claimed["chat_5"] = true }},
		{"accept_method", "GET", base + "/accept", "", nil},
		{"accept_empty", "POST", base + "/accept", `{"id":"wb-cn","task_codes":[]}`, nil},
		{"accept_duplicate", "POST", base + "/accept", `{"id":"wb-cn","task_codes":["chat_5","chat_5"]}`, nil},
		{"accept_invalid_code", "POST", base + "/accept", `{"id":"wb-cn","task_codes":["../task"]}`, nil},
		{"accept_unknown", "POST", base + "/accept", `{"id":"wb-cn","task_codes":["missing"]}`, nil},
		{"accept_two", "POST", base + "/accept", `{"id":"wb-cn","task_codes":["chat_5","Model_chat_GLM5.2"]}`, nil},
		{"accept_already", "POST", base + "/accept", `{"id":"wb-cn","task_codes":["chat_5"]}`, func(s *Server, f *growthActionFixture) { f.accepted["chat_5"] = true }},
		{"accept_prerequisite", "POST", base + "/accept", `{"id":"wb-cn","task_codes":["chat_5"]}`, func(s *Server, f *growthActionFixture) { f.prerequisite = true }},
		{"run_method", "GET", base + "/run", "", nil},
		{"run_unknown", "POST", base + "/run", `{"id":"wb-cn","task_code":"unsupported"}`, nil},
		{"run_no_model", "POST", base + "/run", paid, func(s *Server, f *growthActionFixture) { s.Reg.ReplaceProvider("workbuddy", "cn", nil) }},
		{"run_paid", "POST", base + "/run", paid, nil},
		{"run_partial_stream", "POST", base + "/run", paid, func(s *Server, f *growthActionFixture) { f.brokenStream = true }},
		{"run_corrupt_state", "POST", base + "/run", paid, func(s *Server, f *growthActionFixture) {
			if err := os.WriteFile(s.growthExecutionPath(), []byte(`invalid`), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"run_activity", "POST", base + "/run", activity, func(s *Server, f *growthActionFixture) { f.accepted["chat_5"] = true; f.progress["chat_5"] = 4 }},
		{"run_activity_failure", "POST", base + "/run", activity, func(s *Server, f *growthActionFixture) { f.accepted["chat_5"] = true; f.reportFailure = true }},
		{"run_invalid_target", "POST", base + "/run", activity, func(s *Server, f *growthActionFixture) { f.accepted["chat_5"] = true; f.targets["chat_5"] = 6 }},
		{"run_disabled", "POST", base + "/run", paid, func(s *Server, f *growthActionFixture) { s.Store.SetAccountEnabled("wb-cn", false) }},
		{"run_claim_ready", "POST", base + "/run", paid, func(s *Server, f *growthActionFixture) {
			f.accepted["Model_chat_GLM5.2"] = true
			f.progress["Model_chat_GLM5.2"] = 1
		}},
		{"run_buddy_consent", "POST", base + "/run", `{"id":"wb-cn","task_code":"first_buddy"}`, func(s *Server, f *growthActionFixture) { f.buddies = true; f.targets["first_buddy"] = 1 }},
		{"run_buddy_owned", "POST", base + "/run", `{"id":"wb-cn","task_code":"first_buddy"}`, func(s *Server, f *growthActionFixture) {
			f.buddies = true
			f.owned = true
			f.targets["first_buddy"] = 1
		}},
	}
	var results []map[string]any
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newGrowthActionFixture()
			s := growthActionsServer(t, f)
			if tc.setup != nil {
				tc.setup(s, f)
			}
			w := growthRequest(s, tc.method, tc.path, tc.body)
			var response any
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err, w.Body.String())
			}
			if w.Code == 401 {
				t.Fatal("fixture did not authenticate")
			}
			f.mu.Lock()
			counts := map[string]int{"chats": f.chats, "reports": f.reports, "claims": f.claims, "accepts": f.accepts, "adoptions": f.adoptions, "agreements": f.agreements}
			f.mu.Unlock()
			var checkpoint any
			if b, err := os.ReadFile(s.growthExecutionPath()); err == nil {
				if json.Unmarshal(b, &checkpoint) == nil {
					state := checkpoint.(map[string]any)
					for _, m := range state["marks"].(map[string]any) {
						m.(map[string]any)["at"] = float64(0)
					}
				} else {
					checkpoint = "corrupt"
				}
			}
			var usage []map[string]any
			for _, entry := range s.Store.Logs(10) {
				usage = append(usage, map[string]any{"kind": entry.RequestKind, "protocol": entry.Protocol, "model": entry.Model, "attempts": entry.Attempts, "status": entry.Status, "known": entry.Usage.Known, "input": entry.Usage.InputTokens, "output": entry.Usage.OutputTokens})
			}
			results = append(results, map[string]any{"case": tc.name, "status": w.Code, "response": response, "calls": counts, "checkpoint": checkpoint, "usage": usage, "in_flight": s.Store.PoolSnapshot()["wb-cn"].InFlight})
		})
	}
	b, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("GROWTH_CONTRACT " + string(b))
}

func benchmarkGrowthHTTP(b *testing.B, claimed bool) {
	st, err := store.Open(filepath.Join(b.TempDir(), "state.json"))
	if err != nil {
		b.Fatal(err)
	}
	raw, _ := json.Marshal(workbuddy.Credentials{AccessToken: "fixture-token", UID: "fixture-user", Realm: "cn", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	st.UpsertAccount(&accounts.Account{ID: "wb-cn", Provider: "workbuddy", Scope: "cn", ExternalID: "fixture-user", Enabled: true, Credentials: raw})
	s := newConfiguredServer(st)
	adapter, _ := s.Providers.Get("workbuddy")
	status := "accepted"
	if claimed {
		status = "claimed"
	}
	response := fmt.Sprintf(`{"code":0,"data":{"tasks":[{"task_code":"Model_chat_GLM5.2","target":1,"current":1,"accept_status":%q}]}}`, status)
	adapter.(*workbuddy.Client).HTTP = &http.Client{Transport: probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
	})}
	invoke := func() {
		w := httptest.NewRecorder()
		if claimed {
			s.handleGrowthRun(w, httptest.NewRequest("POST", "/admin/api/workbuddy/tasks/run", strings.NewReader(`{"id":"wb-cn","task_code":"Model_chat_GLM5.2"}`)))
		} else {
			s.handleGrowthTasks(w, httptest.NewRequest("GET", "/admin/api/workbuddy/tasks?id=wb-cn", nil))
		}
		if w.Code != 200 {
			b.Fatal(w.Code, w.Body.String())
		}
	}
	invoke()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		invoke()
	}
}
func BenchmarkGrowthListHTTP(b *testing.B)    { benchmarkGrowthHTTP(b, false) }
func BenchmarkGrowthClaimedHTTP(b *testing.B) { benchmarkGrowthHTTP(b, true) }
