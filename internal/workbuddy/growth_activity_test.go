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

func TestGrowthActivityUsesMaintenanceWireWithoutChat(t *testing.T) {
	var ids []string
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v2/report" || r.Header.Get("User-Agent") != "WorkBuddy/5.5.4" || r.Header.Get("Accept-Language") != "zh-CN" || r.Header.Get("X-CodeBuddy-Request") != "1" {
			t.Error("incorrect maintenance wire", r.Method, r.URL.Path, r.Header.Get("User-Agent"))
		}
		for _, h := range []string{"X-Client-Platform", "X-Conversation-ID", "X-Machine-ID", "X-IDE-Name"} {
			if r.Header.Get(h) != "" {
				t.Error("chat or web headers leaked", h)
			}
		}
		var events []map[string]any
		if json.NewDecoder(r.Body).Decode(&events) != nil || len(events) != 1 {
			t.Fatal("expected one activity event")
		}
		e := events[0]
		id, _ := e["requestId"].(string)
		if !strings.HasPrefix(id, "wb2api-") || e["conversationId"] != id || e["userId"] != r.Header.Get("X-User-Id") || e["eventCode"] != "chat_request_send" || e["requestModelId"] != "deepseek-v4-flash" {
			t.Error("incorrect event identity", e)
		}
		for _, field := range []string{"mentionContexts", "knowledgeId", "knowledgeName", "isPlan", "isAutoModify", "maxSteps", "maxToken", "agentType", "rootRequestId", "parentConversationId"} {
			if _, ok := e[field]; !ok {
				t.Error("missing reference field", field)
			}
		}
		ids = append(ids, id)
		io.WriteString(w, `{"code":0,"data":null}`)
	}))
	defer billing.Close()
	c := New()
	c.CNBilling, c.CNBase = billing.URL, "http://chat-must-not-be-called.invalid"
	for _, code := range []string{"first_buddy", "chat_5"} {
		if err := c.ReportGrowthActivity(context.Background(), fixtureAuth("cn"), code); err != nil {
			t.Fatal(err)
		}
		if _, err := c.RunGrowthChat(context.Background(), fixtureAuth("cn"), code, func([]byte) error { t.Fatal("activity invoked model"); return nil }); err == nil {
			t.Fatal("activity task accepted by billable executor")
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatal("activity reports share an identity")
	}
	for _, code := range []string{"Model_chat_GLM5.2", "automation_1", "missing"} {
		if c.ReportGrowthActivity(context.Background(), fixtureAuth("cn"), code) == nil {
			t.Fatal("unsupported task reported", code)
		}
	}
}

func TestGrowthBuddyErrorsRemainSpecificWithoutLeakingBodies(t *testing.T) {
	for _, message := range []string{"first_buddy task not completed yet", "fixture-secret"} {
		t.Run(message, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 10000, "msg": message})
			}))
			defer up.Close()
			c := New()
			c.CNBase = up.URL
			err := c.AdoptGrowthBuddy(context.Background(), fixtureAuth("cn"))
			if err == nil || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("error missing or unsafe", err)
			}
			if message != "fixture-secret" && !strings.Contains(err.Error(), "领养门槛尚未达标") {
				t.Fatal("known prerequisite lost", err)
			}
		})
	}
}
