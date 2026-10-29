package websearch

import (
	"codearts2api/internal/provider"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearxngResultBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		count      int
		bad        bool
	}{
		{"results", `{"results":[{"url":"javascript:bad"},{"url":"https://u:p@example.test"},{"url":"https://example.test/a","title":"A","content":"excerpt"},{"url":"https://example.test/a"},{"url":"https://example.test/b"}]}`, 1, false},
		{"empty", `{"results":[]}`, 0, false},
		{"null", `{"results":null}`, 0, true},
		{"missing", `{}`, 0, true},
		{"html", `<html>Forbidden</html>`, 0, true},
		{"oversize", strings.Repeat(" ", (1<<20)+1), 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/search" {
					t.Error("wrong endpoint")
				}
				r.ParseForm()
				if r.Form.Get("q") != "question" || r.Form.Get("format") != "json" {
					t.Error("wrong form")
				}
				io.WriteString(w, tc.body)
			}))
			defer up.Close()
			out, err := New().Search(t.Context(), up.URL, provider.SearchRequest{Query: "question", MaxResults: 1})
			if (err != nil) != tc.bad {
				t.Fatalf("unexpected error %v", err)
			}
			if err == nil && (len(out.Results) != tc.count || out.TotalResults != tc.count) {
				t.Fatal("result filtering")
			}
		})
	}
}
func TestSearxngDoesNotFollowRedirectOrEchoSecrets(t *testing.T) {
	calls := 0
	dest := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer dest.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", dest.URL)
		w.WriteHeader(302)
		io.WriteString(w, "canary-private-token")
	}))
	defer up.Close()
	_, err := New().Search(t.Context(), up.URL, provider.SearchRequest{Query: "question"})
	if err == nil || calls != 0 || strings.Contains(err.Error(), "canary-private") {
		t.Fatal("unsafe redirect/error")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = New().Search(ctx, up.URL, provider.SearchRequest{Query: "question"}); err == nil {
		t.Fatal("cancel ignored")
	}
}
