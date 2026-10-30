package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/workbuddy"
)

func growthRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Admin-Password", "admin-fixture")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestGrowthClaimConcurrentIsolationAndConfirmation(t *testing.T) {
	var claims, balances atomic.Int32
	var claimed atomic.Bool
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case workbuddy.GrowthTasksPath:
			status := "accepted"
			if claimed.Load() {
				status = "claimed"
			}
			fmt.Fprintf(w, `{"code":0,"data":{"tasks":[{"task_code":"chat_5","target":5,"current":5,"accept_status":%q,"reward_credit":100}]}}`, status)
		case "/activity/growth/tasks/chat_5/claim":
			claims.Add(1)
			claimed.Store(true)
			io.WriteString(w, `{"code":0,"data":{"credit":100,"energy":0,"already_claimed":false}}`)
		case "/v2/billing/meter/get-user-resource":
			balances.Add(1)
			io.WriteString(w, `{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CapacityRemain":150,"CapacitySize":150}]}}}}`)
		default:
			t.Errorf("unexpected upstream operation: %s", r.URL.Path)
		}
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	before := s.Store.Account("wb-cn")
	stats, usage := s.Store.Stats(), s.Store.Keys()[0].BudgetUsage
	// Read-only task listing must not cause reward, telemetry, or model calls.
	w := growthRequest(s, "GET", "/admin/api/workbuddy/tasks?id=wb-cn", "")
	if w.Code != 200 || claims.Load() != 0 || balances.Load() != 0 {
		t.Fatal("task listing has side effects", w.Code, w.Body.String())
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := growthRequest(s, "POST", "/admin/api/workbuddy/tasks/claim", `{"id":" wb-cn ","task_code":"chat_5"}`)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "claimed") {
				t.Error(w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 || balances.Load() != 1 {
		t.Fatal("duplicate claims or balance refresh", claims.Load(), balances.Load())
	}
	if !reflect.DeepEqual(before, s.Store.Account("wb-cn")) || !reflect.DeepEqual(stats, s.Store.Stats()) || !reflect.DeepEqual(usage, s.Store.Keys()[0].BudgetUsage) || len(s.Store.Logs(10)) != 0 || key == "" {
		t.Fatal("growth mutated daily markers, inference health, statistics or key budgets")
	}
	if cached, err, ok := s.cachedCredit("wb-cn"); !ok || err != nil || cached.Remaining != 150 {
		t.Fatal("confirmed reward did not refresh balance", cached, err, ok)
	}
}

func TestGrowthRejectsInvalidActionsBeforeWrites(t *testing.T) {
	var writes atomic.Int32
	s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
		}
		io.WriteString(w, `{"code":0,"data":{"tasks":[{"task_code":"not_ready","target":5,"current":4},{"task_code":"locked","target":1,"current":1,"locked":true}]}}`)
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	addWB(t, s, "global", time.Now().Add(time.Hour).Unix())
	s.Store.UpsertAccount(&codearts.Account{ID: "other", Provider: "trae", Enabled: true})
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{`, 400}, {`{}`, 400}, {`{"id":"wb-cn","task_code":"../bad"}`, 400},
		{`{"id":"wb-cn","task_code":"not_ready"} {}`, 400},
		{`{"id":"wb-cn","task_code":"not_ready","extra":1}`, 400},
		{`{"id":"missing","task_code":"x"}`, 404}, {`{"id":"other","task_code":"x"}`, 400},
		{`{"id":"wb-global","task_code":"x"}`, 400}, {`{"id":"wb-cn","task_code":"missing"}`, 404},
		{`{"id":"wb-cn","task_code":"not_ready"}`, 409}, {`{"id":"wb-cn","task_code":"locked"}`, 409},
	} {
		w := growthRequest(s, "POST", "/admin/api/workbuddy/tasks/claim", tc.body)
		if w.Code != tc.status {
			t.Error(tc.body, w.Code, w.Body.String())
		}
	}
	s.Store.SetAccountEnabled("wb-cn", false)
	if w := growthRequest(s, "POST", "/admin/api/workbuddy/tasks/claim", `{"id":"wb-cn","task_code":"not_ready"}`); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/admin/api/workbuddy/tasks", 400}, {"GET", "/admin/api/workbuddy/tasks?id=missing", 404},
		{"POST", "/admin/api/workbuddy/tasks", 405}, {"GET", "/admin/api/workbuddy/tasks/claim", 405},
	} {
		if w := growthRequest(s, tc.method, tc.path, ""); w.Code != tc.status {
			t.Error(tc, w.Code)
		}
	}
	if writes.Load() != 0 {
		t.Fatal("invalid request reached a write endpoint")
	}
	r := httptest.NewRequest("GET", "/admin/api/workbuddy/tasks?id=wb-cn", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("task listing bypassed admin authentication", w.Code)
	}
	r = httptest.NewRequest("POST", "/admin/api/workbuddy/tasks/claim", strings.NewReader(`{"id":"wb-cn","task_code":"x"}`))
	r.Header.Set("X-Admin-Password", "admin-fixture")
	r.Header.Set("Origin", "https://untrusted.invalid")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("task claim bypassed same-origin protection", w.Code)
	}
}

func TestGrowthDoesNotInventSuccessOrRetryUnknownClaims(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			var writes atomic.Int32
			s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					writes.Add(1)
					if malformed {
						io.WriteString(w, `{"code":0,"data":{}}`)
					} else {
						io.WriteString(w, `{"code":0,"data":{"credit":100}}`)
					}
					return
				}
				io.WriteString(w, `{"code":0,"data":{"tasks":[{"task_code":"x","target":1,"current":1}]}}`)
			})
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			w := growthRequest(s, "POST", "/admin/api/workbuddy/tasks/claim", `{"id":"wb-cn","task_code":"x"}`)
			if malformed {
				if w.Code != 502 {
					t.Fatal(w.Code, w.Body.String())
				}
			} else {
				var reply growthReply
				if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.State != "pending" || reply.Reward != nil || reply.Balance != nil {
					t.Fatal("unconfirmed reward reported as successful", w.Body.String())
				}
			}
			if writes.Load() != 1 || s.Store.Account("wb-cn").Failures != 0 || s.Store.Account("wb-cn").LastClaimDate != "" {
				t.Fatal("unknown reward retried or changed inference/daily state")
			}
		})
	}
}

func TestGrowthWaitDoesNotHoldInferenceCredentialLock(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	s, key, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case workbuddy.GrowthTasksPath:
			io.WriteString(w, `{"code":0,"data":{"tasks":[{"task_code":"x","target":1,"current":1}]}}`)
		case "/activity/growth/tasks/x/claim":
			close(started)
			<-finish
			io.WriteString(w, `{"code":0,"data":{}}`)
		case "/v2/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, summaryFixture("NORMAL_REQUEST_OK", "stop", 10, 4))
		default:
			t.Error(r.URL.Path)
		}
	})
	addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		growthRequest(s, "POST", "/admin/api/workbuddy/tasks/claim", `{"id":"wb-cn","task_code":"x"}`)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not start")
	}
	defer func() { close(finish); wg.Wait() }()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- responseRequest(s, key, "/v1/chat/completions", `{"model":"workbuddy/cn/shared","messages":[{"role":"user","content":"hi"}],"max_tokens":8}`)
	}()
	select {
	case w := <-done:
		if w.Code != 200 || !strings.Contains(w.Body.String(), "NORMAL_REQUEST_OK") {
			t.Fatal(w.Code, w.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("growth claim blocked normal inference")
	}
}
