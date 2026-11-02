package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func TestUserProfileHTTPAndCSP(t *testing.T) {
	s, _, g := mappedFixture(t, nil)
	u := createTestUser(t, s, "alice", g.ID)
	createTestUser(t, s, "bob", g.ID)
	cookie := loginTestUser(t, s, "alice")
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	avatar := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData.Bytes())
	body, _ := json.Marshal(map[string]string{"display_name": "用户昵称", "avatar": avatar})
	w := userHTTP(s, cookie, "POST", "/user/api/profile", string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ := s.Store.User(u.ID)
	if got.DisplayName != "用户昵称" || got.Avatar != avatar || got.Username != "alice" || got.Revision != u.Revision {
		t.Fatal("profile semantics", got)
	}
	if userHTTP(s, cookie, "GET", "/user/api/profile", "").Code != 200 {
		t.Fatal("profile edit revoked session")
	}
	for _, body := range []string{`{"max_concurrency":100}`, `{"group_id":"other"}`, `{"user_id":"other","display_name":"x"}`, `{"avatar":"https://example.com/image.png"}`, `{}`, `null`} {
		if w = userHTTP(s, cookie, "POST", "/user/api/profile", body); w.Code != 400 {
			t.Fatal("invalid patch", body, w.Code)
		}
	}
	for _, path := range []string{"/user", "/"} {
		w = httptest.NewRecorder()
		serveConsole(w, httptest.NewRequest("GET", path, nil))
		allowsInline := strings.Contains(w.Header().Get("Content-Security-Policy"), "img-src 'self' data:")
		if allowsInline != (path == "/user") || strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-") {
			t.Fatal("avatar CSP", path)
		}
	}
}

func TestUserDashboardUsesPrivateFullRangeAndTimezone(t *testing.T) {
	s, _, g := mappedFixture(t, nil)
	u := createTestUser(t, s, "alice", g.ID)
	bob := createTestUser(t, s, "bob", g.ID)
	cookie := loginTestUser(t, s, "alice")
	for i := 0; i < 25; i++ {
		owner := u.ID
		if i == 24 {
			owner = bob.ID
		}
		e := storage.LogEntry{UserID: owner, Time: time.Date(2026, 3, 8, 12, i, 0, 0, time.UTC).Unix(), Status: 200, Model: "private-model", PublicModel: "public-4.1", Account: "private-account", RequestID: "request-" + string(rune('a'+i)), Usage: storage.TokenUsage{Known: true, InputTokens: 10, OutputTokens: 2, CachedTokens: 5}}
		if err := s.Store.Record(e, 0); err != nil {
			t.Fatal(err)
		}
	}
	var first, second storage.UsagePage
	for page, target := range map[string]*storage.UsagePage{"1": &first, "2": &second} {
		w := userHTTP(s, cookie, "GET", "/user/api/usage?page="+page+"&page_size=10&timezone=America%2FNew_York", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-model") || strings.Contains(w.Body.String(), "private-account") {
			t.Fatal(w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
			t.Fatal(err)
		}
	}
	if first.Total != 24 || first.Stats.InputTokens != 240 || first.Analysis.TimeZone != "America/New_York" || !reflect.DeepEqual(first.Analysis, second.Analysis) || len(first.Analysis.Models) != 1 || first.Analysis.Models[0].Name != "public-4.1" {
		t.Fatal("dashboard isolation", first)
	}
	if w := userHTTP(s, cookie, "GET", "/user/api/usage?timezone=Not_A_Zone", ""); w.Code != 400 {
		t.Fatal("invalid timezone", w.Code)
	}
}

func TestUserLimitHTTPProtocolsAndRejectedCallsNeverConsume(t *testing.T) {
	s, _, g := mappedFixture(t, nil)
	u := createTestUser(t, s, "alice", g.ID)
	limit := 1
	if _, err := s.Store.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, MaxConcurrency: &limit}, ""); err != nil {
		t.Fatal(err)
	}
	k, err := s.Store.AddUserKeyAndSave(u.ID, "one")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := s.Store.AddUserKeyAndSave(u.ID, "two")
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.Store.BeginKeyBudget(k.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		w := responseRequest(s, k2.Key, path, mappedBody(path, false, mappedPublic))
		if w.Code != 429 || !strings.Contains(w.Body.String(), "rate_limit_error") {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	if s.Store.Users()[0].BudgetUsage.Requests != 0 || len(s.Store.Logs(100)) != 0 {
		t.Fatal("rejected call consumed usage")
	}
	release()
	w := responseRequest(s, k2.Key, "/v1/chat/completions", mappedBody("/v1/chat/completions", false, mappedPublic))
	if w.Code != 200 || s.Store.Users()[0].InFlight != 0 {
		t.Fatal("slot not reusable", w.Code)
	}
}

func TestUserConcurrencyCancellationReleasesAllProtocolSlots(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(path+fmt.Sprint(stream), func(t *testing.T) {
				started := make(chan struct{}, 1)
				s, _, g := mappedFixture(t, func(_ string, w http.ResponseWriter, r *http.Request) {
					started <- struct{}{}
					<-r.Context().Done()
					w.WriteHeader(502)
				})
				onlyMappedPlatform(s, provider.CodeArts)
				u := createTestUser(t, s, "alice", g.ID)
				limit := 1
				if _, err := s.Store.SaveUser(access.UserUpdate{ID: u.ID, Username: u.Username, GroupID: u.GroupID, MaxConcurrency: &limit}, ""); err != nil {
					t.Fatal(err)
				}
				k, err := s.Store.AddUserKeyAndSave(u.ID, "cancel")
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				r := httptest.NewRequest("POST", path, strings.NewReader(mappedBody(path, stream, mappedPublic))).WithContext(ctx)
				r.Header.Set("Authorization", "Bearer "+k.Key)
				done := make(chan struct{})
				go func() { defer close(done); s.Handler().ServeHTTP(httptest.NewRecorder(), r) }()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("upstream not reached")
				}
				if s.Store.Users()[0].InFlight != 1 {
					t.Error("missing slot")
				}
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("cancellation stuck")
				}
				if s.Store.Users()[0].InFlight != 0 {
					t.Fatal("user slot leaked")
				}
				for _, slot := range s.Store.PoolSnapshot() {
					if slot.InFlight != 0 {
						t.Fatal("account slot leaked")
					}
				}
			})
		}
	}
}
