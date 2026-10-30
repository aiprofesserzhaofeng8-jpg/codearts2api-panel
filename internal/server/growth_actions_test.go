package server

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
	"codearts2api/internal/workbuddy"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type growthActionFixture struct {
	mu                                         sync.Mutex
	accepted                                   map[string]bool
	claimed                                    map[string]bool
	progress                                   map[string]int64
	targets                                    map[string]int64
	chats, reports, claims, accepts            int
	ignoreAccept, ignoreProgress, brokenStream bool
	prerequisite                               bool
	hold                                       <-chan struct{}
	started                                    chan struct{}
}

func newGrowthActionFixture() *growthActionFixture {
	return &growthActionFixture{accepted: map[string]bool{}, claimed: map[string]bool{}, progress: map[string]int64{}, targets: map[string]int64{"Model_chat_GLM5.2": 1, "chat_5": 5, "automation_1": 1}}
}
func (f *growthActionFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case workbuddy.GrowthTasksPath:
		var tasks []map[string]any
		for _, code := range []string{"Model_chat_GLM5.2", "chat_5", "automation_1"} {
			status := "not_accepted"
			if f.accepted[code] {
				status = "accepted"
			}
			if f.claimed[code] {
				status = "claimed"
			}
			tasks = append(tasks, map[string]any{"task_code": code, "accept_status": status, "target": f.targets[code], "current": f.progress[code], "reward_credit": 100, "reward_energy": 5})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"tasks": tasks}})
	case workbuddy.GrowthAcceptPath:
		f.accepts++
		if f.prerequisite {
			io.WriteString(w, `{"code":0,"data":{"results":[{"task_code":"chat_5","status":"error","message":"prerequisite not met: first_buddy"}]}}`)
			return
		}
		var body struct {
			Codes []string `json:"task_codes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !f.ignoreAccept {
			for _, code := range body.Codes {
				f.accepted[code] = true
			}
		}
		io.WriteString(w, `{"code":0,"data":{}}`)
	case "/v2/chat/completions":
		f.chats++
		id := f.chats
		if f.started != nil {
			select {
			case f.started <- struct{}{}:
			default:
			}
		}
		if f.hold != nil {
			f.mu.Unlock()
			select {
			case <-f.hold:
			case <-r.Context().Done():
			}
			f.mu.Lock()
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if f.brokenStream {
			io.WriteString(w, "data: {\"id\":\"partial-real-id\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
			return
		}
		fmt.Fprintf(w, "data: {\"id\":\"real-response-%d\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n", id)
	case "/v2/report":
		f.reports++
		var events []map[string]any
		_ = json.NewDecoder(r.Body).Decode(&events)
		if len(events) != 1 || events[0]["requestId"] != fmt.Sprintf("real-response-%d", f.chats) {
			http.Error(w, "no real receipt", 400)
			return
		}
		if !f.ignoreProgress {
			for _, code := range []string{"Model_chat_GLM5.2", "chat_5"} {
				if f.accepted[code] && f.progress[code] < f.targets[code] {
					f.progress[code]++
				}
			}
		}
		io.WriteString(w, `{"code":0,"data":{}}`)
	case "/v2/billing/meter/get-user-resource":
		io.WriteString(w, `{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CapacityRemain":1000,"CapacitySize":1000}]}}}}`)
	default:
		if strings.HasPrefix(r.URL.Path, "/activity/growth/tasks/") && strings.HasSuffix(r.URL.Path, "/claim") {
			code := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/activity/growth/tasks/"), "/claim")
			if !f.accepted[code] || f.progress[code] < f.targets[code] {
				http.Error(w, "not completed", 400)
				return
			}
			f.claims++
			f.claimed[code] = true
			io.WriteString(w, `{"code":0,"data":{"credit":100,"energy":5,"already_claimed":false}}`)
		} else {
			http.Error(w, "unexpected fixture operation", 500)
		}
	}
}
func growthActionsServer(t *testing.T, f *growthActionFixture) *Server {
	t.Helper()
	s, _, _ := nativeWBServer(t, f.serve)
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	s.Reg.ReplaceProvider("workbuddy", "cn", []provider.Model{{ID: "glm-5.2", StreamOnly: true}})
	return s
}
func runGrowthFixture(s *Server, code string) *httptest.ResponseRecorder {
	return growthRequest(s, "POST", "/admin/api/workbuddy/tasks/run", fmt.Sprintf(`{"id":"wb-cn","task_code":%q}`, code))
}
func TestGrowthActionsActualExecutionAccountingAndConcurrentDedup(t *testing.T) {
	f := newGrowthActionFixture()
	s := growthActionsServer(t, f)
	keyBefore := s.Store.Keys()[0].BudgetUsage
	accountBefore := s.Store.Account("wb-cn")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := runGrowthFixture(s, "Model_chat_GLM5.2")
			if w.Code != 200 || !strings.Contains(w.Body.String(), "claimed") {
				t.Error(w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if f.chats != 1 || f.reports != 1 || f.claims != 1 || f.accepts != 1 {
		t.Fatal("duplicate or missing real work", f.chats, f.reports, f.claims, f.accepts)
	}
	logs := s.Store.Logs(10)
	if len(logs) != 1 || logs[0].RequestKind != "growth" || logs[0].Protocol != "growth" || !logs[0].Usage.Known || logs[0].Usage.InputTokens != 10 || logs[0].Usage.OutputTokens != 2 || logs[0].Attempts != 1 {
		t.Fatal("real work not accounted", logs)
	}
	accountAfter := s.Store.Account("wb-cn")
	if accountAfter.LastUsed == 0 {
		t.Fatal("real chat did not update last-used time")
	}
	accountBefore.LastUsed = accountAfter.LastUsed
	if !reflect.DeepEqual(keyBefore, s.Store.Keys()[0].BudgetUsage) || !reflect.DeepEqual(accountBefore, accountAfter) {
		t.Fatal("growth mutated client budget, daily markers or inference health")
	}
	if s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
		t.Fatal("lease leaked")
	}
	info, err := os.Stat(s.growthExecutionPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("execution checkpoint not private", info, err)
	}
}
func TestGrowthActionsOnlyFillConfirmedDeficit(t *testing.T) {
	f := newGrowthActionFixture()
	f.accepted["chat_5"] = true
	f.progress["chat_5"] = 3
	s := growthActionsServer(t, f)
	w := runGrowthFixture(s, "chat_5")
	var reply growthReply
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	if w.Code != 200 || reply.State != "claimed" || reply.CompletedCalls != 2 || f.chats != 2 || f.reports != 2 || f.claims != 1 {
		t.Fatal(w.Code, w.Body.String(), f.chats)
	}
}
func TestGrowthActionsPendingAndPartialFailureDoNotReplayAfterRestart(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			f := newGrowthActionFixture()
			f.accepted["chat_5"] = true
			f.ignoreProgress = true
			f.brokenStream = broken
			s := growthActionsServer(t, f)
			w := runGrowthFixture(s, "chat_5")
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"pending"`) || f.chats != 1 || f.claims != 0 {
				t.Fatal(w.Code, w.Body.String(), f.chats)
			}
			restarted := New(s.Store)
			restarted.Providers = s.Providers
			restarted.Reg = s.Reg
			w = runGrowthFixture(restarted, "chat_5")
			if w.Code != 200 || f.chats != 1 || f.claims != 0 {
				t.Fatal("uncertain call replayed after restart", w.Code, f.chats)
			}
			w = growthRequest(restarted, "GET", "/admin/api/workbuddy/tasks?id=wb-cn", "")
			if !strings.Contains(w.Body.String(), `"execution_pending":true`) {
				t.Fatal("pending guard absent from UI", w.Body.String())
			}
			if !broken {
				f.mu.Lock()
				f.ignoreProgress = false
				f.progress["chat_5"] = 1
				f.mu.Unlock()
				w = runGrowthFixture(restarted, "chat_5")
				if w.Code != 200 || f.chats != 5 || f.claims != 1 {
					t.Fatal("confirmed progress failed to resume remaining calls", w.Code, w.Body.String(), f.chats)
				}
			}
		})
	}
}
func TestGrowthActionsEnrollmentMustBeConfirmedAndTargetsBounded(t *testing.T) {
	f := newGrowthActionFixture()
	f.ignoreAccept = true
	s := growthActionsServer(t, f)
	if w := runGrowthFixture(s, "Model_chat_GLM5.2"); w.Code != 200 || f.chats != 0 || !strings.Contains(w.Body.String(), `"state":"pending"`) {
		t.Fatal("unconfirmed enrollment triggered model", w.Code, w.Body.String())
	}
	f.mu.Lock()
	f.ignoreAccept = false
	f.accepted["chat_5"] = true
	f.targets["chat_5"] = 6
	f.mu.Unlock()
	if w := runGrowthFixture(s, "chat_5"); w.Code != 409 || f.chats != 0 {
		t.Fatal("unbounded model calls", w.Code, w.Body.String())
	}
	for _, code := range []string{"automation_1", "RichMeow_Chat", "skill_1", "first_buddy", "missing"} {
		if w := runGrowthFixture(s, code); w.Code != 409 || f.chats != 0 || f.reports != 0 {
			t.Fatal("unsupported action executed", code, w.Code)
		}
	}
}
func TestGrowthActionsPersistenceAndSharedPoolFailuresBeforeBillableCall(t *testing.T) {
	for _, kind := range []string{"corrupt", "write_failure", "pool_busy"} {
		t.Run(kind, func(t *testing.T) {
			f := newGrowthActionFixture()
			f.accepted["chat_5"] = true
			s := growthActionsServer(t, f)
			switch kind {
			case "corrupt":
				if err := os.WriteFile(s.growthExecutionPath(), []byte(`{"broken":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "write_failure":
				if err := os.Mkdir(s.growthExecutionPath(), 0700); err != nil {
					t.Fatal(err)
				}
				s.growthExecution.loaded = true
				s.growthExecution.marks = map[string]growthExecutionMark{}
			case "pool_busy":
				_, release1 := s.Store.AcquireAccountForModel("", "workbuddy/cn/glm-5.2", nil)
				defer release1()
				_, release2 := s.Store.AcquireAccountForModel("", "workbuddy/cn/glm-5.2", nil)
				defer release2()
			}
			w := runGrowthFixture(s, "chat_5")
			if w.Code != 200 || f.chats != 0 || f.reports != 0 || f.claims != 0 || len(s.Store.Logs(10)) != 0 {
				t.Fatal("precondition failure spent tokens", kind, w.Code, w.Body.String())
			}
		})
	}
}
func TestGrowthActionsMutationGuardsAndEnrollmentIdempotency(t *testing.T) {
	f := newGrowthActionFixture()
	s := growthActionsServer(t, f)
	path := "/admin/api/workbuddy/tasks/accept"
	for _, body := range []string{`{}`, `{"id":"wb-cn","task_codes":[]}`, `{"id":"wb-cn","task_codes":["../x"]}`, `{"id":"wb-cn","task_codes":["chat_5","chat_5"]}`, `{"id":"wb-cn","task_codes":["chat_5"],"extra":1}`, `{"id":"wb-cn","task_codes":["chat_5"]} {}`} {
		if w := growthRequest(s, "POST", path, body); w.Code != 400 {
			t.Fatal("invalid enrollment accepted", w.Code)
		}
	}
	for _, p := range []string{path, "/admin/api/workbuddy/tasks/run"} {
		r := httptest.NewRequest("POST", p, strings.NewReader(`{"id":"wb-cn","task_code":"chat_5"}`))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("admin auth bypass")
		}
		r = httptest.NewRequest("POST", p, strings.NewReader(`{}`))
		r.Header.Set("X-Admin-Password", "admin-fixture")
		r.Header.Set("Origin", "https://evil.invalid")
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("origin bypass")
		}
		if w := growthRequest(s, "GET", p, ""); w.Code != 405 {
			t.Fatal("method guard bypass")
		}
	}
	for i := 0; i < 2; i++ {
		w := growthRequest(s, "POST", path, `{"id":"wb-cn","task_codes":["chat_5","Model_chat_GLM5.2"]}`)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"accepted"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if f.accepts != 1 || f.chats != 0 || f.reports != 0 || f.claims != 0 {
		t.Fatal("enrollment repeats or does unrelated work")
	}
	s.Store.SetAccountEnabled("wb-cn", false)
	if w := runGrowthFixture(s, "chat_5"); w.Code != 409 {
		t.Fatal("disabled account executed")
	}
}
func TestGrowthActionsCancellationStopsRemainingCalls(t *testing.T) {
	f := newGrowthActionFixture()
	f.accepted["chat_5"] = true
	f.started = make(chan struct{}, 1)
	hold := make(chan struct{})
	f.hold = hold
	s := growthActionsServer(t, f)
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/admin/api/workbuddy/tasks/run", strings.NewReader(`{"id":"wb-cn","task_code":"chat_5"}`))
	req.Header.Set("X-Admin-Password", "admin-fixture")
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, _ := server.Client().Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-f.started:
	case <-time.After(2 * time.Second):
		t.Fatal("model never started")
	}
	cancel()
	close(hold)
	<-done
	// Synchronize with the task lock to wait for accounting/cleanup after disconnect.
	release, err := s.growthRuntime.lock(context.Background(), "wb-cn")
	if err != nil {
		t.Fatal(err)
	}
	release()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.chats != 1 || f.reports != 0 || f.claims != 0 {
		t.Fatal("canceled action continued", f.chats, f.reports, f.claims)
	}
}

func TestGrowthActionsRespectPaidModelSwitchWithoutBlockingEligibleClaims(t *testing.T) {
	f := newGrowthActionFixture()
	s := growthActionsServer(t, f)
	s.Store.UpdateSettings(func(settings *store.Settings) { settings.AllowCreditModels = false })
	if w := runGrowthFixture(s, "chat_5"); w.Code != 403 || f.chats != 0 || f.accepts != 0 {
		t.Fatal("paid model switch bypassed", w.Code, w.Body.String())
	}
	f.mu.Lock()
	f.accepted["chat_5"] = true
	f.progress["chat_5"] = 5
	f.mu.Unlock()
	if w := runGrowthFixture(s, "chat_5"); w.Code != 200 || f.claims != 1 || f.chats != 0 {
		t.Fatal("existing reward incorrectly requires paid calls", w.Code, w.Body.String())
	}
}

func TestGrowthActionsExplainRealEnrollmentPrerequisiteWithoutSpending(t *testing.T) {
	f := newGrowthActionFixture()
	f.prerequisite = true
	s := growthActionsServer(t, f)
	for _, w := range []*httptest.ResponseRecorder{runGrowthFixture(s, "chat_5"), growthRequest(s, "POST", "/admin/api/workbuddy/tasks/accept", `{"id":"wb-cn","task_codes":["chat_5"]}`)} {
		if w.Code != 409 || !strings.Contains(w.Body.String(), "领养第一只 Buddy") {
			t.Fatal("prerequisite hidden as pending/success", w.Code, w.Body.String())
		}
	}
	if f.chats != 0 || f.reports != 0 || f.claims != 0 || len(s.Store.Logs(10)) != 0 {
		t.Fatal("rejected enrollment spent credits")
	}
}
