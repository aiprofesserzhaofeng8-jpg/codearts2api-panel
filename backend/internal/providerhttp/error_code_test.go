package providerhttp

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

func TestFailureResponseCodeIsBoundedAndDoesNotChangePolicy(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{`{"code":"C400999","message":"secret request contents"}`, "C400999"},
		{`{"code":0,"error":{"code":"MODEL_NOT_AVAILABLE","message":"sk-secret"}}`, "MODEL_NOT_AVAILABLE"},
		{`{"data":{"error_code":4023}}`, "4023"},
		{`{"code":"sk-secret","message":"secret"}`, ""},
		{`{"code":"https://example.com/private"}`, ""},
		{`{"code":"SK_SECRET"}`, ""},
		{`{"code":null}`, ""},
		{strings.Repeat(" ", 16<<10) + `{"code":4023}`, ""},
	} {
		resp := &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
		err := FailureResponseWithCode("Fixture", resp).(*provider.Error)
		if err.Code != tc.code || err.Status != 403 || err.AccountFault || err.ContentBlocked || strings.Contains(err.Message, "secret") {
			t.Fatalf("unsafe diagnostic: %+v", err)
		}
	}
	resp := &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader(`{"code":"QUOTA_EXCEEDED"}`))}
	if err := FailureResponseWithCode("Fixture", resp).(*provider.Error); err.RetryAt < time.Now().Unix()+59 {
		t.Fatal("retry metadata lost")
	}
}
