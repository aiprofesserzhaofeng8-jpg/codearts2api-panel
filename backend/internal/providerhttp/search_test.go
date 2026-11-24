package providerhttp

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

func TestSearchResponseFailures(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		resp := &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"60"}}, Body: io.NopCloser(strings.NewReader("secret"))}
		_, err := SearchJSON(resp, "fixture")
		var failure *provider.Error
		if !errors.As(err, &failure) || failure.Status != status || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
		if status == 429 && failure.RetryAt == 0 {
			t.Fatal("lost retry-after")
		}
	}
	_, err := SearchJSON(&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+1)))}, "fixture")
	if err == nil {
		t.Fatal("oversized response accepted")
	}
	for _, input := range []provider.SearchRequest{{Query: "x"}, {Query: "valid", MaxResults: -1}, {Query: strings.Repeat("x", 4097)}} {
		if _, _, err := SearchInput(input); err == nil {
			t.Fatal("invalid query accepted")
		}
	}
}
