package server

import (
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func buddyFixture(t *testing.T) (*Server, *growthActionFixture) {
	t.Helper()
	f := newGrowthActionFixture()
	f.buddies = true
	return growthActionsServer(t, f), f
}

func runBuddyFixture(t *testing.T, s *Server, code string, consent bool) growthReply {
	t.Helper()
	w := growthRequest(s, "POST", "/admin/api/workbuddy/tasks/run", fmt.Sprintf(`{"id":"wb-cn","task_code":%q,"agree_buddy":%t}`, code, consent))
	var reply growthReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return reply
}

func TestGrowthBuddyConsentAndReadOnlyStatus(t *testing.T) {
	s, f := buddyFixture(t)
	w := growthRequest(s, "GET", "/admin/api/workbuddy/tasks?id=wb-cn", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"buddy":{"owned":false,"agreed":false}`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, code := range []string{"first_buddy", "chat_5"} {
		w = growthRequest(s, "POST", "/admin/api/workbuddy/tasks/run", fmt.Sprintf(`{"id":"wb-cn","task_code":%q}`, code))
		if w.Code != 409 || !strings.Contains(w.Body.String(), "勾选同意") {
			t.Fatal("consent not enforced before paid work", w.Code, w.Body.String())
		}
	}
	if f.chats+f.reports+f.agreements+f.adoptions+f.claims+f.accepts != 0 {
		t.Fatal("query or missing consent caused writes")
	}
}

func TestGrowthBuddyDoesNotRequireEnrollment(t *testing.T) {
	s, f := buddyFixture(t)
	w := growthRequest(s, "GET", "/admin/api/workbuddy/tasks?id=wb-cn", "")
	var reply growthReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || !growthTask(reply.Tasks, "first_buddy").AcceptNotRequired || growthTask(reply.Tasks, "chat_5").AcceptNotRequired {
		t.Fatal("incorrect enrollment capability", w.Code, w.Body.String())
	}
	w = growthRequest(s, "POST", "/admin/api/workbuddy/tasks/accept", `{"id":"wb-cn","task_codes":["first_buddy"]}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "无需接取") || f.accepts+f.chats+f.agreements+f.adoptions != 0 {
		t.Fatal("unnecessary enrollment reached upstream", w.Code, w.Body.String())
	}
	if runBuddyFixture(t, s, "first_buddy", true).State != "claimed" {
		t.Fatal("enrollment rejection blocked explicit onboarding")
	}
}

func TestGrowthBuddyPreparesDependencyBeforeEnrollingAndExecuting(t *testing.T) {
	s, f := buddyFixture(t)
	budget := s.Store.Keys()[0].BudgetUsage
	if w := growthRequest(s, "POST", "/admin/api/workbuddy/tasks/accept", `{"id":"wb-cn","task_codes":["chat_5"]}`); w.Code != 409 {
		t.Fatal("fixture must start with a rejected dependency")
	}
	reply := runBuddyFixture(t, s, "chat_5", true)
	if reply.State != "claimed" || reply.Buddy == nil || !reply.Buddy.Owned || reply.CompletedCalls != 6 || f.chats != 6 || f.reports != 6 || f.agreements != 1 || f.adoptions != 1 || f.claims != 2 {
		t.Fatal("dependency did not complete once before real task", reply, f.chats, f.reports, f.agreements, f.adoptions, f.claims)
	}
	logs := s.Store.Logs(20)
	if len(logs) != 6 || !reflect.DeepEqual(budget, s.Store.Keys()[0].BudgetUsage) {
		t.Fatal("real calls not isolated from client key budget", len(logs))
	}
	for _, log := range logs {
		if log.Protocol != "growth" || !log.Usage.Known || log.Model != provider.ModelID(provider.WorkBuddy, "cn", "glm-5.2") {
			t.Fatal("unaccounted onboarding request", log)
		}
	}
	if s.Store.PoolSnapshot()["wb-cn"].InFlight != 0 {
		t.Fatal("onboarding leaked account lease")
	}
}

func TestGrowthBuddyConcurrentActionsAdoptOnlyOnce(t *testing.T) {
	s, f := buddyFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); runBuddyFixture(t, s, "first_buddy", true) }()
	}
	wg.Wait()
	if f.chats != 1 || f.reports != 1 || f.agreements != 1 || f.adoptions != 1 || f.claims != 1 {
		t.Fatal("same account performed duplicate onboarding", f.chats, f.agreements, f.adoptions, f.claims)
	}
}

func TestGrowthBuddyUnknownAdoptionRecoversWithoutAnotherPaidChat(t *testing.T) {
	s, f := buddyFixture(t)
	f.ignoreAdoption = true
	first := runBuddyFixture(t, s, "first_buddy", true)
	if first.State != "pending" || first.CompletedCalls != 1 || f.chats != 1 || f.adoptions != 1 || f.claims != 0 {
		t.Fatal("unconfirmed ownership treated as success", first)
	}
	f.mu.Lock()
	f.ignoreAdoption = false
	f.mu.Unlock()
	restarted := New(s.Store)
	restarted.Providers, restarted.Reg = s.Providers, s.Reg
	second := runBuddyFixture(t, restarted, "first_buddy", true)
	if second.State != "claimed" || second.CompletedCalls != 0 || f.chats != 1 || f.reports != 1 || f.agreements != 1 || f.adoptions != 2 || f.claims != 1 {
		t.Fatal("explicit recovery repeated the paid prerequisite", second, f.chats, f.reports, f.adoptions)
	}
}

func TestGrowthBuddyDoesNotAdoptAfterUnverifiedAgreementOrChat(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			s, f := buddyFixture(t)
			f.ignoreAgreement = !broken
			f.brokenStream = broken
			reply := runBuddyFixture(t, s, "chat_5", true)
			if reply.State != "pending" || f.chats != 1 || f.adoptions != 0 || f.claims != 0 || f.accepts != 0 {
				t.Fatal("unknown prerequisite started dependent work", reply, f.chats, f.adoptions, f.accepts)
			}
			if broken && f.reports != 0 {
				t.Fatal("broken chat produced a completion report")
			}
		})
	}
}

func TestGrowthBuddyExistingOwnershipAndLostAcknowledgement(t *testing.T) {
	t.Run("already owned without paid models", func(t *testing.T) {
		s, f := buddyFixture(t)
		f.owned = true
		s.Store.UpdateSettings(func(settings *store.Settings) { settings.AllowCreditModels = false })
		reply := runBuddyFixture(t, s, "first_buddy", false)
		if reply.State != "prepared" || reply.Buddy == nil || !reply.Buddy.Owned || f.chats+f.reports+f.agreements+f.adoptions != 0 {
			t.Fatal("existing ownership caused new actions", reply)
		}
	})
	t.Run("adoption committed but response failed", func(t *testing.T) {
		s, f := buddyFixture(t)
		f.adoptErrorAfterCommit = true
		reply := runBuddyFixture(t, s, "first_buddy", true)
		if reply.State != "claimed" || reply.Buddy == nil || !reply.Buddy.Owned || f.adoptions != 1 || f.chats != 1 {
			t.Fatal("lost acknowledgement repeated or concealed adoption", reply)
		}
	})
}

func TestGrowthBuddyCreditSwitchAndCheckpointFailureStopBeforeWrites(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			s, f := buddyFixture(t)
			if corrupt {
				if err := os.WriteFile(s.growthExecutionPath(), []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				s.Store.UpdateSettings(func(settings *store.Settings) { settings.AllowCreditModels = false })
			}
			w := growthRequest(s, "POST", "/admin/api/workbuddy/tasks/run", `{"id":"wb-cn","task_code":"first_buddy","agree_buddy":true}`)
			if !corrupt && w.Code != 403 || corrupt && w.Code != 200 || f.chats+f.reports+f.agreements+f.adoptions+f.claims != 0 {
				t.Fatal("unsafe onboarding write", w.Code, w.Body.String())
			}
		})
	}
}

func TestGrowthBuddyRejectsOtherProvidersWithoutChangingTheirAccounts(t *testing.T) {
	s, f := buddyFixture(t)
	for _, id := range []string{"trae", "codearts"} {
		a := &codearts.Account{ID: id, Provider: id, Enabled: true, Name: "isolation fixture"}
		s.Store.UpsertAccount(a)
		before := s.Store.Account(id)
		w := growthRequest(s, "POST", "/admin/api/workbuddy/tasks/run", fmt.Sprintf(`{"id":%q,"task_code":"first_buddy","agree_buddy":true}`, id))
		if w.Code != 400 || !reflect.DeepEqual(before, s.Store.Account(id)) {
			t.Fatal("other provider affected", id, w.Code)
		}
	}
	if f.chats+f.reports+f.agreements+f.adoptions+f.accepts+f.claims != 0 {
		t.Fatal("other provider reached WorkBuddy actions")
	}
}
