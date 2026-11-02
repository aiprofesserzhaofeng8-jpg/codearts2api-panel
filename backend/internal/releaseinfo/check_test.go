package releaseinfo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(tag string) string {
	return fmt.Sprintf(`{"tag_name":%q,"assets":[{"name":"SHA256SUMS","browser_download_url":%q},{"name":"panelctl.py","browser_download_url":%q},{"name":%q,"browser_download_url":%q}]}`, tag, ReleasesURL+"/download/"+tag+"/SHA256SUMS", ReleasesURL+"/download/"+tag+"/panelctl.py", "codearts2api-panel-"+tag+"-linux-"+runtime.GOARCH+".tar.gz", ReleasesURL+"/download/"+tag+"/codearts2api-panel-"+tag+"-linux-"+runtime.GOARCH+".tar.gz")
}
func TestVersionsAndIncompleteRelease(t *testing.T) {
	for _, tc := range []struct{ current, body, state string }{
		{"v0.3.0", fixture("v0.4.0"), "available"}, {"v0.4.0", fixture("v0.4.0"), "current"},
		{"v0.5.0", fixture("v0.4.0"), "ahead"}, {"0.3.0-user-controls", fixture("v0.4.0"), "development"},
		{"v0.1.0", `{"tag_name":"v0.4.0","assets":[]}`, "incomplete"},
		{"v0.1.0", `{"tag_name":"v0.4.0","prerelease":true}`, "unavailable"},
		{"v0.1.0", strings.ReplaceAll(fixture("v0.4.0"), "https://github.com/", "https://evil.invalid/"), "unavailable"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			c := New(tc.current)
			calls := 0
			c.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != endpoint {
					t.Fatal("unexpected URL")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			s := c.Check(context.Background())
			if s.State != tc.state {
				t.Fatalf("%+v", s)
			}
			c.Check(context.Background())
			if calls != 1 {
				t.Fatal("not cached")
			}
		})
	}
}
func TestBoundedUnavailableAndConcurrentCheck(t *testing.T) {
	c := New("v0.1.0")
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	c.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		<-release
		return nil, context.DeadlineExceeded
	})
	done := make(chan Status, 1)
	go func() { done <- c.Check(context.Background()) }()
	<-entered
	if s := c.Check(context.Background()); s.State != "busy" {
		t.Fatal(s)
	}
	close(release)
	if s := <-done; s.State != "unavailable" {
		t.Fatal(s)
	}
	if c.Check(context.Background()).State != "unavailable" || calls.Load() != 1 {
		t.Fatal("failure not cached")
	}
	for _, raw := range []string{strings.Repeat("x", 2*1024*1024+1), "not json"} {
		c.expires = time.Time{}
		c.Client.Transport = transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, nil
		})
		if c.Check(context.Background()).State != "unavailable" {
			t.Fatal("bad response accepted")
		}
	}
}

func TestLimitedAndUnpublished(t *testing.T) {
	for _, tc := range []struct {
		code  int
		state string
	}{{403, "limited"}, {429, "limited"}, {404, "unpublished"}, {502, "unavailable"}} {
		c := New("v0.3.1")
		c.Client.Transport = transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader("private upstream body"))}, nil
		})
		if s := c.Check(context.Background()); s.State != tc.state || strings.Contains(s.Message, "private") {
			t.Fatal(s)
		}
	}
}
