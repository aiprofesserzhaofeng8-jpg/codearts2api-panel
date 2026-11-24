package websearch

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeepSeekStandalone(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantCount  int
	}{
		{"success", 200, `{"results":[{"url":"javascript:x"},{"url":"https://example.org/1","summary":"long","snippet":"short"},{"url":"https://example.org/1"},{"url":"https://example.org/2","snippet":"fallback"}]}`, 0, 2},
		{"empty", 200, `{"results":[]}`, 0, 0}, {"missing", 200, `{}`, 502, 0}, {"malformed", 200, `[`, 502, 0}, {"oversized", 200, strings.Repeat(" ", 1<<20+1), 502, 0},
		{"auth", 401, "sensitive", 502, 0}, {"balance", 402, "sensitive", 502, 0}, {"limit", 429, "sensitive", 429, 0}, {"server", 500, "sensitive", 502, 0}, {"redirect", 307, "sensitive", 502, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			calls := 0
			query := strings.Repeat("查", 128)
			c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" || r.URL.String() != deepSeekSearchURL || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Fatal("incorrect outbound request")
				}
				var in map[string]any
				if json.NewDecoder(r.Body).Decode(&in) != nil || in["query"] != query || in["count"] != float64(5) || in["need_content"] != false {
					t.Fatal("incorrect payload")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://other.example"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			out, err := c.DeepSeek(t.Context(), "fixture", provider.SearchRequest{Query: query})
			if calls != 1 {
				t.Fatal("unexpected request count", calls)
			}
			if tc.wantStatus != 0 {
				var e *provider.Error
				if !errors.As(err, &e) || e.Status != tc.wantStatus || strings.Contains(e.Message, "sensitive") {
					t.Fatalf("unexpected error %v", err)
				}
				return
			}
			if err != nil || out.TotalResults != tc.wantCount || out.Results == nil {
				t.Fatalf("unexpected result %v %v", out, err)
			}
			if len(out.Results) == 2 && (out.Results[0].Snippet != "long" || out.Results[1].Snippet != "fallback") {
				t.Fatal("incorrect excerpts")
			}
		})
	}
}
func TestDeepSeekRejectsInvalidQueryBeforeHTTP(t *testing.T) {
	c := New()
	c.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid query reached transport")
		return nil, nil
	})
	for _, q := range []string{"   ", strings.Repeat("查", 129)} {
		if _, err := c.DeepSeek(t.Context(), "fixture", provider.SearchRequest{Query: q}); err == nil {
			t.Fatal("accepted invalid query")
		}
	}
}
