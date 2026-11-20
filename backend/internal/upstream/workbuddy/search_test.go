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

func TestSearchWireContract(t *testing.T) {
	for _, scope := range []string{"cn", "global"} {
		t.Run(scope, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/agenttool/v1/search" {
					t.Error("wrong search route")
				}
				for key, want := range map[string]string{"Content-Type": "application/json;charset=UTF-8", "Accept": "application/json", "Authorization": "Bearer fixture-access", "X-Agent-Intent": "craft", "X-Agent-Runtime": "other", "X-Agent-Purpose": "conversation", "X-Conversation-ID": "conversation", "X-Conversation-Request-ID": "conversation-request", "X-Trace-ID": "trace", "X-CodeBuddy-Request": "1"} {
					if r.Header.Get(key) != want {
						t.Errorf("%s: %q", key, r.Header.Get(key))
					}
				}
				if r.Header.Get("X-Refresh-Token") != "" {
					t.Error("refresh token leaked")
				}
				var body struct {
					Query      string `json:"query"`
					Type       string `json:"type"`
					MaxResults int    `json:"max_results"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body.Query != "测试搜索" || body.Type != "text2text" || body.MaxResults != 10 {
					t.Errorf("wrong query: %+v", body)
				}
				io.WriteString(w, `{"query":"测试搜索","results":[{"title":"result","url":"https://example.test/a","snippet":"text"},{"url":"javascript:alert(1)"}],"total_results":2}`)
			}))
			defer up.Close()
			c := New()
			c.CNBase = up.URL
			c.GlobalBase = up.URL
			out, err := c.Search(context.Background(), fixtureAuth(scope), provider.SearchRequest{Query: " 测试搜索 ", MaxResults: 99, SessionID: "conversation", ConversationRequestID: "conversation-request", TraceID: "trace"})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Results) != 1 || out.TotalResults != 1 || out.Results[0].Snippet != "text" {
				t.Fatalf("bad result %+v", out)
			}
		})
	}
}
func TestSearchMalformedEmptyAndErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		ok     bool
	}{
		{200, `{"results":[]}`, true}, {200, `{}`, false}, {200, `{"results":null}`, false}, {200, `not json`, false},
		{200, `{"code":15003,"msg":"fixture-access fixture-refresh"}`, false},
		{429, `{"msg":"fixture-access fixture-refresh"}`, false},
		{200, strings.Repeat("x", (1<<20)+1), false},
	} {
		t.Run(string(rune(tc.status))+tc.body[:min(15, len(tc.body))], func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
			defer up.Close()
			c := New()
			c.CNBase = up.URL
			out, err := c.Search(context.Background(), fixtureAuth("cn"), provider.SearchRequest{Query: "test"})
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected result: %+v %v", out, err)
			}
			if err != nil && (strings.Contains(err.Error(), "fixture-access") || strings.Contains(err.Error(), "fixture-refresh")) {
				t.Fatal("error leaked credentials")
			}
		})
	}
}
func TestSearchCancellationAndRedirect(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer target.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer up.Close()
	c := New()
	c.CNBase = up.URL
	if _, err := c.Search(context.Background(), fixtureAuth("cn"), provider.SearchRequest{Query: "test"}); err == nil || leaked {
		t.Fatal("credential redirect followed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if _, err := c.Search(ctx, fixtureAuth("cn"), provider.SearchRequest{Query: "test"}); err == nil {
		t.Fatal("cancellation ignored")
	}
}
