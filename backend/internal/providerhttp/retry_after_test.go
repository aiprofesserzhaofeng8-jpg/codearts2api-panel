package providerhttp

import (
	"net/http"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

func TestFailureResponseRetryAfter(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		status int
		value  string
		known  bool
	}{{429, "120", true}, {503, now.Add(time.Hour).UTC().Format(http.TimeFormat), true}, {429, "0", false}, {429, "-10", false}, {429, "9999999999999999999", false}, {429, "invalid", false}, {429, now.Add(-time.Hour).UTC().Format(http.TimeFormat), false}, {400, "120", false}, {401, "120", false}} {
		e := FailureResponse("fixture", &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{tc.value}}}).(*provider.Error)
		if (e.RetryAt > now.Unix()) != tc.known || e.Status != tc.status || e.AccountFault != (tc.status == 401) {
			t.Fatal(tc, e)
		}
	}
}
