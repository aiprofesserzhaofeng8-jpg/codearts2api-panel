package workbuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

func TestGrowthDesktopAndWebActivitiesUseSeparateWires(t *testing.T) {
	desktopCalls, webCalls := 0, 0
	desktop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		desktopCalls++
		if r.Header.Get("User-Agent") != growthDesktopUA || r.Header.Get("X-Product") != "SaaS" || r.Header.Get("X-Client-Platform") != "" {
			t.Error("desktop headers mixed", r.Header)
		}
		if r.URL.Path == "/v2/user-asset/appearance/set" {
			io.WriteString(w, `{"code":0,"data":null}`)
			return
		}
		if r.URL.Path != "/v2/report" {
			t.Error("activity called model", r.URL.Path)
		}
		var events []map[string]any
		if json.NewDecoder(r.Body).Decode(&events) != nil || len(events) == 0 {
			t.Fatal("missing event group")
		}
		for _, e := range events {
			if e["extName"] != "workbuddy-desktop" || e["ideName"] != "WorkBuddy" || e["userId"] != r.Header.Get("X-User-Id") {
				t.Error("missing event profile", e)
			}
		}
		io.WriteString(w, `{"code":0,"data":null}`)
	}))
	defer desktop.Close()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webCalls++
		if r.URL.Path != "/v2/report" || r.Header.Get("X-Client-Platform") != "web" || r.Header.Get("X-IDE-Name") != "" || !strings.Contains(r.Header.Get("Referer"), "/space/d/") {
			t.Error("wrong web wire")
		}
		var events []map[string]any
		_ = json.NewDecoder(r.Body).Decode(&events)
		if len(events) != 1 || events[0]["elementId"] != "library_doc_intro_click" {
			t.Error("wrong library event")
		}
		io.WriteString(w, `{"code":0,"data":null}`)
	}))
	defer web.Close()
	c := New()
	c.CNBase = desktop.URL
	c.CNWeb = web.URL
	c.CNBilling = "http://must-not-use-billing.invalid"
	for _, code := range []string{"RichMeow_Chat", "Buddy_App", "Buddy_App_QQ", "automation_1", "Library_read", "playbook_prompt", "create_canvas", "Hp_Appearance", "template_5"} {
		action, ok := c.GrowthAction(code)
		if !ok || action.MaxCalls != 0 || action.MaxReports == 0 {
			t.Fatal("event task marked billable", code)
		}
		if err := c.ReportGrowthActivity(context.Background(), fixtureAuth("cn"), provider.GrowthTask{Code: code, Current: 2}); err != nil {
			t.Fatal(code, err)
		}
	}
	if desktopCalls != 9 || webCalls != 1 {
		t.Fatal(desktopCalls, webCalls)
	}
}
func TestGrowthExpertsResolveOfficialIDAndReportOnlyCompleteRealChats(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			chats, reports, summons := 0, 0, 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/portal/operation-platform/market/expert/list":
					io.WriteString(w, `{"code":0,"data":{"experts":[{"expert_id":"ex_wrong","expert_type":"agent","display_name_zh":"中国电商运营专家"},{"expert_id":"ex_official","expert_type":"agent","display_name_zh":"卖得好"}]}}`)
				case "/v2/chat/completions":
					chats++
					if r.Header.Get("X-Expert-Id") != "ex_official" || r.Header.Get("User-Agent") != growthDesktopUA {
						t.Error("wrong expert or headers")
					}
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["model"] != "fast-model" || body["max_tokens"] != float64(32) {
						t.Error("unbounded model")
					}
					io.WriteString(w, "data: {\"id\":\"real-server-id\",\"choices\":[{\"delta\":{\"content\":\"2\"}}]}\n\n")
					if !partial {
						io.WriteString(w, "data: {\"id\":\"real-server-id\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
					}
				case "/v2/report":
					var events []map[string]any
					_ = json.NewDecoder(r.Body).Decode(&events)
					for _, e := range events {
						if e["eventCode"] == "expert_summon_click" {
							summons++
						}
						if e["eventCode"] == "expert_actual_use" {
							reports++
							if e["id"] != "ex_official" || e["requestId"] != "real-server-id" {
								t.Error("invented expert or response")
							}
						}
					}
					io.WriteString(w, `{"code":0,"data":null}`)
				default:
					t.Error("unexpected request", r.URL.Path)
				}
			}))
			defer up.Close()
			c := New()
			c.CNBase = up.URL
			task := provider.GrowthTask{Code: "expert_use_maidehao", JumpURL: "https://www.workbuddy.cn/work/launch/?deeplink=workbuddy%3A%2F%2Fexperts%3FexpertId%3Dex_official", Target: 1}
			plan, err := c.PrepareGrowthChat(context.Background(), fixtureAuth("cn"), task)
			if err != nil {
				t.Fatal(err)
			}
			completed, err := c.RunGrowthChatStep(context.Background(), fixtureAuth("cn"), plan, func([]byte) error { return nil })
			if chats != 1 || summons != 1 || partial && (err == nil || reports != 0) || !partial && (err != nil || !completed || reports != 1) {
				t.Fatal(completed, err, chats, reports, summons)
			}
			task.JumpURL = "https://untrusted.invalid/?expertId=ex_official"
			if _, err = c.PrepareGrowthChat(context.Background(), fixtureAuth("cn"), task); err == nil {
				t.Fatal("untrusted resource accepted")
			}
		})
	}
}
