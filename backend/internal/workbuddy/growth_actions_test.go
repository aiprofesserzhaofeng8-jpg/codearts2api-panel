package workbuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestGrowthActionsRequireRealChatAndCorrectWire(t *testing.T) {
	var calls []string
	var conversation string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Header.Get("X-Client-Platform") != "" {
			t.Error("desktop/mp/web claim headers leaked into enrollment or chat")
		}
		switch r.URL.Path {
		case GrowthAcceptPath:
			var body struct {
				Codes []string `json:"task_codes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body.Codes, []string{"Model_chat_GLM5.2"}) {
				t.Error("wrong enrollment body")
			}
			io.WriteString(w, `{"code":0,"data":null}`)
		case "/v2/chat/completions":
			var body struct {
				Model string `json:"model"`
				Max   int    `json:"max_tokens"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Model != "glm-5.2" || body.Max != 32 {
				t.Error("wrong model/budget")
			}
			conversation = r.Header.Get("X-Conversation-ID")
			io.WriteString(w, "data: {\"id\":\"real-response-id\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
		case "/v2/report":
			var events []map[string]any
			_ = json.NewDecoder(r.Body).Decode(&events)
			if len(calls) != 3 || len(events) != 1 || events[0]["eventCode"] != "chat_request_send" || events[0]["requestId"] != "real-response-id" || events[0]["conversationId"] != conversation || events[0]["requestModelId"] != "glm-5.2" {
				t.Error("report is not tied to completed chat")
			}
			if events[0]["inputLength"] != float64(len([]rune(growthPrompt))) || events[0]["maxToken"] != float64(32) {
				t.Error("report invents chat parameters")
			}
			io.WriteString(w, `{"code":0}`)
		default:
			t.Error("unexpected operation", r.URL.Path)
		}
	}))
	defer up.Close()
	c := New()
	c.CNBase = up.URL
	c.CNBilling = up.URL
	c.CNWeb = "http://web-must-not-be-used.invalid"
	if err := c.AcceptGrowthTasks(context.Background(), fixtureAuth("cn"), []string{"Model_chat_GLM5.2"}); err != nil {
		t.Fatal(err)
	}
	chunks := 0
	completed, err := c.RunGrowthChat(context.Background(), fixtureAuth("cn"), "Model_chat_GLM5.2", func([]byte) error { chunks++; return nil })
	if err != nil || !completed || chunks != 1 || len(calls) != 3 {
		t.Fatal(completed, err, calls, chunks)
	}
	for _, code := range []string{"wb_wechat_oa_subscribe_task", "Expert_Philanthropy", "missing"} {
		if _, ok := c.GrowthAction(code); ok {
			t.Error("unsupported task advertised as executable", code)
		}
	}
}
func TestGrowthActionsNeverReportFailedOrUnverifiableChats(t *testing.T) {
	for i, stream := range []string{
		"data: [DONE]\n\n",
		"data: {\"id\":\"real\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\n",
		"data: {\"id\":\"real\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thinking\"},\"finish_reason\":\"length\"}]}\n\n",
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			reports := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/report" {
					reports++
					io.WriteString(w, `{"code":0,"data":{}}`)
				} else {
					io.WriteString(w, stream)
				}
			}))
			defer up.Close()
			c := New()
			c.CNBase = up.URL
			if _, err := c.RunGrowthChat(context.Background(), fixtureAuth("cn"), "Model_chat_GLM5.2", func([]byte) error { return nil }); err == nil {
				t.Error("invalid chat accepted")
			}
			if reports != 0 {
				t.Error("failed/unverifiable chat reported successful")
			}
		})
	}
}
func TestGrowthEnrollmentValidationAndLocalCapabilities(t *testing.T) {
	c := New()
	c.CNBase = "http://invalid.invalid"
	for _, codes := range [][]string{nil, {"../x"}, {"chat_5", "chat_5"}, make([]string, 21)} {
		if c.AcceptGrowthTasks(context.Background(), fixtureAuth("cn"), codes) == nil {
			t.Error("invalid enrollment accepted")
		}
	}
	tasks, err := parseGrowthTasks([]byte(`{"tasks":[{"task_code":"unsupported","auto_supported":true,"activity_only":true,"action_description":"untrusted"},{"task_code":"Model_chat_GLM5.2","auto_supported":false,"activity_only":true}]}`))
	if err != nil || tasks[0].AutoSupported || tasks[0].ActivityOnly || !tasks[1].AutoSupported || tasks[1].ActivityOnly || strings.Contains(tasks[0].ActionDescription, "untrusted") {
		t.Fatal("local capability trusted upstream metadata", tasks, err)
	}
	tasks, err = parseGrowthTasks([]byte(`{"tasks":[{"task_code":"first_buddy","accept_not_required":false},{"task_code":"chat_5","accept_not_required":true}]}`))
	if err != nil || !tasks[0].AcceptNotRequired || tasks[1].AcceptNotRequired || !tasks[0].ActivityOnly || !tasks[1].ActivityOnly {
		t.Fatal("local enrollment capability trusted upstream metadata", tasks, err)
	}
	err = validateGrowthEnrollment([]byte(`{"results":[{"task_code":"first_buddy","status":"error","message":"task does not require acceptance"}]}`), []string{"first_buddy"})
	if err == nil || !strings.Contains(err.Error(), "无需接取") {
		t.Fatal("unnecessary enrollment mislabeled as prerequisite refusal", err)
	}
}

func TestGrowthEnrollmentDetectsIndividualRejectionsInsideSuccessEnvelope(t *testing.T) {
	for _, tc := range []struct {
		data               string
		fail, prerequisite bool
	}{
		{`{"results":[{"task_code":"chat_5","status":"accepted"}]}`, false, false},
		{`{"results":[{"task_code":"chat_5","status":"error","message":"prerequisite not met: first_buddy"}]}`, true, true},
		{`{"results":[{"task_code":"chat_5","status":"error","message":"fixture-secret"}]}`, true, false},
		{`{"results":[{"task_code":"other","status":"accepted"}]}`, true, false},
		{`{"results":[]}`, true, false},
		{`{"results":[{"task_code":"chat_5","status":"accepted"},{"task_code":"chat_5","status":"accepted"}]}`, true, false},
	} {
		err := validateGrowthEnrollment([]byte(tc.data), []string{"chat_5"})
		if (err != nil) != tc.fail {
			t.Fatal(tc.data, err)
		}
		if err != nil {
			if strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("upstream message leaked")
			}
			if tc.prerequisite && !strings.Contains(err.Error(), "领养第一只 Buddy") {
				t.Fatal("missing prerequisite instruction", err)
			}
		}
	}
}
