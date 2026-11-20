package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"codearts2api/internal/upstream/qoder"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAccountModelSyncDoesNotChangeGroupRouting(t *testing.T) {
	const allowed = "qoder/cn/qmodel"
	const listed = "qoder/cn/catalog-only"
	var chats atomic.Int32
	s, _, _ := nativeQoderServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.RequestURI() == qoder.ModelsPath:
			fmt.Fprint(w, `{"chat":[{"key":"catalog-only"},{"key":"restricted"}]}`)
		case r.URL.Path == strings.SplitN(qoder.ChatPath, "?", 2)[0] && r.Header.Get("X-Model-Key") == "qmodel":
			chats.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, qoderFrame(qoderText)+qoderFrame("[DONE]")+"event: finish\ndata: {}\n\n")
		default:
			t.Errorf("unexpected upstream request %s", r.URL.Path)
		}
	})
	addQoder(t, s, "cn")
	s.Reg.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "qmodel", StreamOnly: true}, {ID: "restricted", StreamOnly: true}})
	g, err := s.Store.SaveGroup(storage.AccountGroup{Name: "limited", AccountIDs: []string{"qoder-cn"}, AllowlistEnabled: true, ModelIDs: []string{allowed}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddKeyInGroupAndSave("limited", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Handler()
	markModelsFresh(s)
	before := modelIDsFromResponse(t, scopedModelRequest(s, k.Key))
	if !reflect.DeepEqual(before, []string{allowed}) {
		t.Fatal("unexpected baseline", before)
	}
	req := httptest.NewRequest("POST", "/admin/api/accounts/models?id=qoder-cn", nil)
	req.AddCookie(adminTestCookie(s, "admin-fixture"))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"catalog-only"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if after := modelIDsFromResponse(t, scopedModelRequest(s, k.Key)); !reflect.DeepEqual(after, before) {
		t.Fatal("sync changed visible models", after)
	}
	for _, tc := range []struct {
		id   string
		want int
	}{{allowed, 200}, {listed, 403}, {"qoder/cn/restricted", 403}} {
		w := responseRequest(s, k.Key, "/v1/chat/completions", fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"test"}]}`, tc.id))
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.id, w.Code, w.Body.String())
		}
	}
	if chats.Load() != 1 {
		t.Fatal("unexpected inference calls", chats.Load())
	}
}

func TestAccountModelSyncLifecycle(t *testing.T) {
	var calls atomic.Int32
	s, key, _ := nativeQoderServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != qoder.ModelsPath {
			t.Errorf("unexpected upstream %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		switch calls.Add(1) {
		case 1:
			fmt.Fprint(w, `{"chat":[{"key":"x","name":"X"},{"key":"y","name":"Y"}]}`)
		case 2:
			fmt.Fprint(w, `{"chat":[{"key":"y","name":"Y"}]}`)
		default:
			http.Error(w, "failed", 502)
		}
	})
	addQoder(t, s, "cn")
	path := "/admin/api/accounts/models?id=qoder-cn"
	if w := responseRequest(s, key, path, "{}"); w.Code != 401 {
		t.Fatal("non-admin accessed sync", w.Code)
	}
	call := func(method string, want int) *accounts.ModelSnapshot {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(adminTestCookie(s, "admin-fixture"))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
		var result struct{ Snapshot *accounts.ModelSnapshot }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Snapshot
	}
	if call("GET", 200) != nil || calls.Load() != 0 {
		t.Fatal("read queried upstream")
	}
	first := call("POST", 200)
	if first == nil || !first.First || len(first.List.Models) != 2 {
		t.Fatal(first)
	}
	next := call("POST", 200)
	if next.First || !reflect.DeepEqual(next.Delta.Missing, []string{"x"}) || !reflect.DeepEqual(next.Delta.Retained, []string{"y"}) {
		t.Fatal(next)
	}
	call("POST", 502)
	if !reflect.DeepEqual(next, call("GET", 200)) {
		t.Fatal("failed sync replaced last success")
	}
	reopened, err := storage.Open(s.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	target, got := reopened.ModelSyncTarget("qoder-cn")
	if !reflect.DeepEqual(next, got) {
		t.Fatal("lost on restart")
	}
	if _, err = reopened.RemoveAccountAndSave("qoder-cn"); err != nil {
		t.Fatal(err)
	}
	if err = reopened.SaveModelSnapshot(target, next); err != accounts.ErrCredentialChanged {
		t.Fatal("stale result committed", err)
	}
	if _, v := reopened.ModelSyncTarget("qoder-cn"); v != nil {
		t.Fatal("deleted snapshot retained")
	}
}
