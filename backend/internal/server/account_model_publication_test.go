package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestSyncedCodexModelPublicationAndLease(t *testing.T) {
	const raw = "gptastra6.2"
	const full = "chatgpt/global/" + raw
	reads, chats := 0, 0
	s, key, g, _ := subscriptionServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/codex/models":
			reads++
			if reads == 1 {
				fmt.Fprint(w, `{"models":[{"slug":"gptastra6.2"}]}`)
			} else {
				fmt.Fprint(w, `{"models":[]}`)
			}
		case "/codex/responses":
			chats++
			var body struct{ Model string }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Model != raw || r.Header.Get("ChatGPT-Account-ID") != "account-fixture" {
				t.Errorf("incorrect forwarding: %q %q", body.Model, r.Header.Get("ChatGPT-Account-ID"))
			}
			subscriptionComplete(w, provider.ChatGPT, false)
		default:
			t.Errorf("unexpected upstream %s", r.URL.Path)
		}
	})
	addSubscription(t, s, g, false, time.Now().Add(time.Hour))
	braw, id, err := g.Normalize(mustJSON(map[string]any{"access_token": "other-access", "refresh_token": "other-refresh", "account_id": "other-account", "user_id": "other-user", "expires_at": time.Now().Add(time.Hour).Unix()}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.UpsertAccountAndSave(&accounts.Account{ID: "b", Provider: provider.ChatGPT, Scope: "global", ExternalID: id.ID, Enabled: true, Credentials: braw}, false); err != nil {
		t.Fatal(err)
	}
	group, err := s.Store.SaveGroup(storage.AccountGroup{Name: "only B", AccountIDs: []string{"b"}, AllowlistEnabled: true, ModelIDs: []string{full}})
	if err != nil {
		t.Fatal(err)
	}
	bk, err := s.Store.AddKeyInGroupAndSave("only B", group.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Handler()
	markModelsFresh(s)
	invoke := func(key, name string) int {
		return responseRequest(s, key, "/v1/responses", fmt.Sprintf(`{"model":%q,"input":"hello"}`, name)).Code
	}
	if invoke(key, raw) != 404 {
		t.Fatal("unknown name resolved before sync")
	}
	syncModels := func() {
		req := httptest.NewRequest("POST", "/admin/api/accounts/models?id=chatgpt-fixture", nil)
		req.AddCookie(adminTestCookie(s, "admin-fixture"))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	syncModels()
	if !slices.Contains(modelIDsFromResponse(t, scopedModelRequest(s, key)), full) || slices.Contains(modelIDsFromResponse(t, scopedModelRequest(s, bk.Key)), full) {
		t.Fatal("wrong listing")
	}
	if code := invoke(key, raw); code != 200 {
		t.Fatal("bare synced ID", code)
	}
	syncModels()
	if code := invoke(key, full); code != 200 {
		t.Fatal("missing from latest list", code)
	}
	reopened, err := storage.Open(s.Store.Path())
	if err != nil || reopened.PublishedModels()[full].SyncedOwners["chatgpt-fixture"] != true {
		t.Fatal("mapping lost on restart", err)
	}
	s.Reg.ReplaceProvider(provider.ChatGPT, "global", []provider.Model{{ID: raw}})
	if code := invoke(bk.Key, full); code != 403 {
		t.Fatal("wrong account authorized", code)
	}
	s.Store.SetAccountEnabled("chatgpt-fixture", false)
	if invoke(key, full) == 200 || chats != 2 {
		t.Fatal("fell through to B", chats)
	}
}
