package workbuddy

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixtureAuth(realm string) json.RawMessage {
	raw, _ := json.Marshal(Credentials{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", UID: "fixture-uid", Realm: realm, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	return raw
}
func TestNormalizeNestedAndExpiry(t *testing.T) {
	c := New()
	raw := `{"auth":{"accessToken":"fixture-access","refreshToken":"fixture-refresh","expiresAt":1900000000000,"realm":"global"},"account":{"uid":"user-1","nickname":"fixture"},"device_token":"fixture-device"}`
	norm, id, err := c.Normalize([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := decode(norm)
	if id.ID != "user-1" || id.Scope != "global" || id.ExpiresAt != 1900000000 || a.DeviceToken != "fixture-device" {
		t.Fatal("nested credentials not normalized")
	}
	for _, bad := range []string{`{}`, `{"accessToken":"x","uid":"u","expiresAt":10,"realm":"evil"}`} {
		if _, _, err := c.Normalize([]byte(bad)); err == nil {
			t.Fatal("bad credentials accepted")
		}
	}
}
func TestNativeRequestsNeverLeakRefreshToken(t *testing.T) {
	for _, realm := range []string{"cn", "global"} {
		t.Run(realm, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-access" || r.Header.Get("X-Refresh-Token") != "" {
					t.Error("credential boundary failed")
				}
				if len(r.Header.Get("X-Conversation-ID")) != 32 || len(r.Header.Get("X-Conversation-Request-ID")) != 32 || r.Header.Get("X-CodeBuddy-Request") != "1" {
					t.Error("missing upstream routing headers")
				}
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req["model"] != "test-model" || req["stream"] != true {
					t.Error("wrong native request")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			c := New()
			c.CNBase = server.URL
			c.GlobalBase = server.URL
			var chunks [][]byte
			err := c.Stream(context.Background(), fixtureAuth(realm), provider.Request{Model: "test-model", Body: []byte(`{"model":"workbuddy/cn/test-model","messages":[{"role":"user","content":"hi"}]}`)}, func(b []byte) error { chunks = append(chunks, b); return nil })
			if err != nil {
				t.Fatal(err)
			}
			if len(chunks) != 1 || !strings.Contains(string(chunks[0]), "workbuddy/"+realm+"/test-model") {
				t.Fatal("response model lost provider namespace")
			}
		})
	}
}
func TestStreamRejectsTruncationAndBusinessErrors(t *testing.T) {
	for _, body := range []string{"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: [DONE]\n\n", "data: {\"code\":12153,\"msg\":\"fixture-access fixture-refresh\"}\n\n", "data: [DONE]\n\n"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		c := New()
		c.CNBase = server.URL
		err := c.Stream(context.Background(), fixtureAuth("cn"), provider.Request{Model: "m", Body: []byte(`{}`)}, func([]byte) error { return nil })
		server.Close()
		if err == nil {
			t.Fatal("invalid stream accepted")
		}
		if strings.Contains(err.Error(), "fixture-access") || strings.Contains(err.Error(), "fixture-refresh") {
			t.Fatal("upstream error leaked token")
		}
	}
}
func TestCredentialRedirectIsNotFollowed(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer origin.Close()
	c := New()
	c.CNBase = origin.URL
	_, err := c.Models(context.Background(), fixtureAuth("cn"))
	if err == nil || calls != 0 {
		t.Fatal("credential-bearing redirect followed")
	}
}
func TestRefreshOnlySendsRefreshCredentialToRefreshPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/plugin/auth/token/refresh" || r.Header.Get("X-Refresh-Token") != "fixture-refresh" || r.Header.Get("Authorization") != "" {
			t.Error("bad refresh request")
		}
		io.WriteString(w, `{"code":0,"data":{"accessToken":"new-access","refreshToken":"new-refresh","expiresIn":7200}}`)
	}))
	defer server.Close()
	c := New()
	c.CNBase = server.URL
	raw, err := c.Refresh(context.Background(), fixtureAuth("cn"))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := decode(raw)
	if a.AccessToken != "new-access" || a.UID != "fixture-uid" {
		t.Fatal("refresh lost identity")
	}
}
func TestBrowserLoginPendingAndPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			io.WriteString(w, `{"code":0,"data":{"state":"fixture-state","authUrl":"https://www.codebuddy.cn/login?state=fixture-state"}}`)
		case "/v2/plugin/auth/token":
			io.WriteString(w, `{"code":1001,"msg":"login ing"}`)
		}
	}))
	defer server.Close()
	c := New()
	c.CNBase = server.URL
	f, err := c.BeginLogin(context.Background(), "cn")
	if err != nil || f.State != "fixture-state" {
		t.Fatal("login start failed")
	}
	_, ready, err := c.CheckLogin(context.Background(), "cn", f.State)
	if err != nil || ready {
		t.Fatal("pending login should not fail or save credentials")
	}
}
