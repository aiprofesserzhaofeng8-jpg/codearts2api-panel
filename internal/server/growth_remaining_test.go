package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/workbuddy"
)

func TestGrowthRemainingActivitiesAndExpertUseAccounting(t *testing.T) {
	for _, code := range []string{"automation_1", "RichMeow_Chat", "expert_5", "skill_1"} {
		t.Run(code, func(t *testing.T) {
			f := newGrowthActionFixture()
			current, target := int64(0), int64(1)
			if code == "expert_5" {
				current, target = 4, 5
			}
			chats, reports, claims := 0, 0, 0
			s, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
				f.mu.Lock()
				defer f.mu.Unlock()
				switch r.URL.Path {
				case workbuddy.GrowthTasksPath:
					fmt.Fprintf(w, `{"code":0,"data":{"tasks":[{"task_code":%q,"accept_status":%q,"current":%d,"target":%d}]}}`, code, map[bool]string{true: "claimed", false: "accepted"}[claims > 0], current, target)
				case "/portal/operation-platform/market/expert/list":
					var entries []map[string]any
					for i := 0; i < 5; i++ {
						entries = append(entries, map[string]any{"expert_id": fmt.Sprint("ex_", i), "expert_type": "agent", "display_name_zh": "fixture"})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"experts": entries}})
				case "/v2/chat/completions":
					chats++
					if code == "expert_5" && r.Header.Get("X-Expert-Id") != "ex_4" {
						t.Error("confirmed deficit did not select next expert")
					}
					io.WriteString(w, "data: {\"id\":\"actual-native-id\",\"choices\":[{\"delta\":{\"content\":\"2\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
				case "/v2/report":
					reports++
					var events []map[string]any
					_ = json.NewDecoder(r.Body).Decode(&events)
					for _, e := range events {
						event := e["eventCode"]
						if code == "automation_1" && event == "automated_task_create_suc" || code == "RichMeow_Chat" && event == "chat_message_response" || code == "expert_5" && event == "expert_actual_use" || code == "skill_1" && event == "skill_info" {
							current++
						}
					}
					io.WriteString(w, `{"code":0,"data":null}`)
				case "/activity/growth/tasks/" + code + "/claim":
					claims++
					io.WriteString(w, `{"code":0,"data":{"credit":100,"energy":5,"already_claimed":false}}`)
				case "/v2/billing/meter/get-user-resource":
					io.WriteString(w, `{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CapacityRemain":1000,"CapacitySize":1000}]}}}}`)
				default:
					t.Error("unexpected request", r.URL.Path)
					http.Error(w, "fixture", 500)
				}
			})
			addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
			// fast-model is a native task alias, not a public model-list entry.
			s.Reg.ReplaceProvider("workbuddy", "cn", []provider.Model{})
			budget := s.Store.Keys()[0].BudgetUsage
			w := runGrowthFixture(s, code)
			var reply growthReply
			_ = json.Unmarshal(w.Body.Bytes(), &reply)
			if w.Code != 200 || reply.State != "claimed" || claims != 1 {
				t.Fatal(w.Code, w.Body.String(), claims)
			}
			paid := code == "expert_5" || code == "skill_1"
			if paid && (chats != 1 || len(s.Store.Logs(10)) != 1 || !s.Store.Logs(10)[0].Usage.Known) || !paid && (chats != 0 || len(s.Store.Logs(10)) != 0) {
				t.Fatal("accounting mixed activity and real chat", chats, reports, s.Store.Logs(10))
			}
			if !reflect.DeepEqual(budget, s.Store.Keys()[0].BudgetUsage) || s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
				t.Fatal("client budget or lease changed")
			}
			again := runGrowthFixture(s, code)
			if again.Code != 200 || claims != 1 || paid && chats != 1 {
				t.Fatal("completed action replayed", again.Code, claims, chats)
			}
		})
	}
}
