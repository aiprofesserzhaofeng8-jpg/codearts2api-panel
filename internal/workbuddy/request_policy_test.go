package workbuddy

import (
	"codearts2api/internal/provider"
	"testing"
)

func TestPolicyContentErrorsAreNotAllChannelErrors(t *testing.T) {
	for _, tc := range []struct {
		body    string
		status  int
		blocked bool
	}{
		{`{"code":11128,"msg":"request illegal"}`, 400, false},
		{`{"code":11140,"msg":"request illegal"}`, 403, false},
		{`{"code":11128,"msg":"blocked by security policy"}`, 400, true},
		{`{"code":11128,"msg":"blocked by security policy"}`, 403, true},
		{`{"code":11128,"msg":"blocked by security policy"}`, 401, false},
	} {
		err := upstreamError(tc.status, []byte(tc.body))
		if err == nil {
			t.Fatal("expected error")
		}
		e := err.(*provider.Error)
		if e.ContentBlocked != tc.blocked || tc.blocked && e.AccountFault {
			t.Fatalf("classification: %+v", e)
		}
	}
}
