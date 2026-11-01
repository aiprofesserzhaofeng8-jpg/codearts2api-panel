package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGrowthWireAndStrictResults(t *testing.T) {
	var methods []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fixture-access" || r.Header.Get("X-Refresh-Token") != "" {
			t.Error("growth credentials are incorrect")
		}
		if r.Method == "GET" {
			if r.Header.Get("X-Client-Platform") != "" {
				t.Error("web claim header leaked into list")
			}
			io.WriteString(w, `{"code":0,"data":{"tasks":[{"task_code":"Model_chat_GLM5.2","reward_credit":100,"reward_energy":5,"current":99,"target":99,"progress":{"current":0,"target":1},"accept_status":"accepted"}]}}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 || r.Header.Get("Origin") != "https://www.workbuddy.cn" || r.Header.Get("X-Client-Platform") != "web" {
			t.Error("wrong web claim contract")
		}
		io.WriteString(w, `{"code":0,"data":{"already_claimed":false,"credit":100,"energy":5}}`)
	}))
	defer up.Close()
	c := New()
	c.CNBase, c.CNWeb = up.URL, up.URL
	list, err := c.ListGrowthTasks(context.Background(), fixtureAuth("cn"))
	if err != nil || len(list) != 1 || list[0].Current != 0 || list[0].Target != 1 || list[0].Claimable || list[0].Credit != 100 {
		t.Fatalf("explicit progress was not preserved: %+v, %v", list, err)
	}
	reward, err := c.ClaimGrowthReward(context.Background(), fixtureAuth("cn"), "Model_chat_GLM5.2")
	if err != nil || reward.Credit != 100 || reward.Energy != 5 || reward.AlreadyClaimed {
		t.Fatalf("wrong reward: %+v, %v", reward, err)
	}
	if strings.Join(methods, ",") != "GET /v2/activity/growth/tasks,POST /activity/growth/tasks/Model_chat_GLM5.2/claim" {
		t.Fatal(methods)
	}
}

func TestGrowthRejectsAmbiguousOrUnsafePayloads(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"data":{"tasks":[]}}`, `{"code":0,"data":null}`, `{"code":0,"data":{}}`,
		`{"code":0,"data":{"tasks":null}}`, `{"code":0,"data":{"tasks":{}}}`,
		`{"code":0,"data":{"tasks":[{"task_code":"../bad"}]}}`,
		`{"code":0,"data":{"tasks":[{"task_code":"x","progress":{"target":"1"}}]}}`,
		`{"code":0,"data":{"tasks":[{"task_code":"x","current":-1}]}}`,
		`{"code":0,"data":{"tasks":[{"task_code":"x"},{"task_code":"x"}]}}`,
		`{"code":12153,"msg":"secret-credential-must-not-appear","data":{"tasks":[]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			defer up.Close()
			c := New()
			c.CNBase = up.URL
			if _, err := c.ListGrowthTasks(context.Background(), fixtureAuth("cn")); err == nil || strings.Contains(err.Error(), "secret-credential") {
				t.Fatal("ambiguous payload accepted or raw upstream data exposed", err)
			}
		})
	}
}

func TestGrowthEligibilityAndClaimability(t *testing.T) {
	c := New()
	if c.SupportsGrowthTasks(fixtureAuth("global")) || c.SupportsGrowthTasks(nil) {
		t.Fatal("unsupported account accepted")
	}
	var enterprise map[string]any
	_ = json.Unmarshal(fixtureAuth("cn"), &enterprise)
	enterprise["enterprise_id"] = "enterprise"
	raw, _ := json.Marshal(enterprise)
	if c.SupportsGrowthTasks(raw) {
		t.Fatal("enterprise growth should be unavailable")
	}
	list, err := parseGrowthTasks([]byte(`{"tasks":[
		{"task_code":"ready","target":1,"current":1,"accept_status":"accepted"},
		{"task_code":"locked","target":1,"current":1,"locked":true},
		{"task_code":"unaccepted","target":1,"current":1,"accept_status":"not_accepted"},
		{"task_code":"claimed","target":1,"current":1,"accept_status":"claimed"},
		{"task_code":"unknown","current":1}
	]}`))
	if err != nil || len(list) != 5 {
		t.Fatal(list, err)
	}
	for i, task := range list {
		if task.Claimable != (i == 0) {
			t.Fatalf("unsafe claim eligibility: %+v", task)
		}
	}
	for _, code := range []string{"", "../bad", "..", "a/b", "a?b", "a#b", strings.Repeat("a", 129)} {
		if _, err := c.ClaimGrowthReward(context.Background(), fixtureAuth("cn"), code); err == nil {
			t.Fatal("unsafe task code accepted", code)
		}
	}
}

func TestGrowthClaimRequiresExplicitRewardResult(t *testing.T) {
	for _, data := range []string{`{}`, `null`, `{"credit":-1}`, `{"already_claimed":true,"credit":100}`, `{"already_claimed":false,"credit":100,"energy":5}`} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"code":0,"data":`+data+`}`) }))
		c := New()
		c.CNWeb = up.URL
		r, err := c.ClaimGrowthReward(context.Background(), fixtureAuth("cn"), "task")
		up.Close()
		if data == `{}` || data == `null` || data == `{"credit":-1}` {
			if err == nil {
				t.Fatal("ambiguous claim accepted", data)
			}
		} else if err != nil || r.AlreadyClaimed && r.Credit != 0 {
			t.Fatal("wrong already claimed accounting", r, err)
		}
	}
}
