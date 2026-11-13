package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"codearts2api/internal/catalog/modelmeta"
)

type referenceTransport func(*http.Request) (*http.Response, error)

func (f referenceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReferenceFetchIsPublicFixedAndBounded(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{{"ok", `{"provider":{"models":{}}}`, 200, false}, {"forbidden", "private upstream error", 403, true}, {"too large", strings.Repeat("x", modelmeta.BodyLimit+1), 200, true}} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: referenceTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != modelmeta.RegistryURL || r.Method != "GET" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept") != "application/json" {
					t.Fatal("reference request leaked state or changed endpoint", r.URL, r.Header)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			_, err := fetchPublicModelReferences(context.Background(), client)
			if (err != nil) != tc.wantError || (err != nil && strings.Contains(err.Error(), "private upstream error")) {
				t.Fatal(err)
			}
		})
	}
}
