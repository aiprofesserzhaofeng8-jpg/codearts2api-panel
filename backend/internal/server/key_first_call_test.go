package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

// Exercise the contract between the key guide, the public API, and the user's
// usage record. The gateway uses its normal routing with a fixture upstream.
func TestKeyGuideModelCompletesFirstCallAcrossProtocols(t *testing.T) {
	var upstreamCalls atomic.Int32
	s, _, group := mappedFixture(t, func(platform string, w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		var req struct{ Stream bool }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("invalid upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		policySuccess(w, platform, req.Stream || platform != provider.CodeArts)
	})
	onlyMappedPlatform(s, provider.WorkBuddy)
	user := createTestUser(t, s, "first-call", group.ID)
	key, err := s.Store.AddUserKeyAndSave(user.ID, "guide")
	if err != nil {
		t.Fatal(err)
	}
	session := loginTestUser(t, s, user.Username)
	path := "/user/api/keys/models?ref=" + keyReference(key.Key)
	w := userHTTP(s, session, http.MethodGet, path, "")
	var guide struct{ Models []string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &guide) != nil || !slices.Equal(guide.Models, []string{mappedPublic}) {
		t.Fatalf("unexpected guide response: %d, models=%v", w.Code, guide.Models)
	}
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), key.Key) {
		t.Fatal("key guide exposed credential data or became cacheable")
	}
	if upstreamCalls.Load() != 0 || len(s.Store.Logs(10)) != 0 {
		t.Fatal("opening the guide used an upstream request")
	}

	model := guide.Models[0]
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(endpoint, func(t *testing.T) {
			w := responseRequest(s, key.Key, endpoint, mappedBody(endpoint, false, model))
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"model":"`+model+`"`) || !strings.Contains(w.Body.String(), `"Hello"`) {
				t.Fatalf("guide model did not complete inference: %d %s", w.Code, w.Body.String())
			}
			log := s.Store.Logs(1)[0]
			if log.UserID != user.ID || log.Model != "workbuddy/cn/shared" || !log.Usage.Known || log.Usage.InputTokens != 12 {
				t.Fatalf("incorrect usage attribution: user_match=%t model=%s usage=%+v", log.UserID == user.ID, log.Model, log.Usage)
			}
		})
	}
	if upstreamCalls.Load() != 3 || len(s.Store.Logs(10)) != 3 {
		t.Fatal("expected one upstream request and one usage entry per protocol")
	}
	if err := s.Store.DeleteGroup(group.ID); err != nil {
		t.Fatal(err)
	}
	checkModelError(t, userHTTP(s, session, http.MethodGet, path, ""), 403, "group_unavailable")
	checkModelError(t, responseRequest(s, key.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, model)), 403, "group_unavailable")
	if upstreamCalls.Load() != 3 {
		t.Fatal("saved request continued to reach upstream after group deletion")
	}
	const otherModel = "second-client-v2"
	const raw = "workbuddy/cn/shared"
	next, err := s.Store.SaveGroup(storage.AccountGroup{Name: "B", AccountIDs: []string{"wb-cn"}, AllowlistEnabled: true, ModelIDs: []string{raw}, ModelMappings: []storage.ModelMapping{{Name: otherModel, Sources: []string{raw}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.SaveUser(access.UserUpdate{ID: user.ID, Username: user.Username, GroupID: next.ID}, ""); err != nil {
		t.Fatal(err)
	}
	session = loginTestUser(t, s, user.Username)
	w = userHTTP(s, session, http.MethodGet, path, "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &guide) != nil || !slices.Equal(guide.Models, []string{otherModel}) {
		t.Fatal("wrong rebound catalog", w.Code, guide.Models)
	}
	const chat = "/v1/chat/completions"
	checkModelError(t, responseRequest(s, key.Key, chat, mappedBody(chat, false, model)), 403, "model_permission_denied")
	w = responseRequest(s, key.Key, chat, mappedBody(chat, false, otherModel))
	if w.Code != 200 || upstreamCalls.Load() != 4 {
		t.Fatal("rebound request", w.Code, upstreamCalls.Load(), w.Body.String())
	}

}
