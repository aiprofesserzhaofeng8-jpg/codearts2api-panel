package trae

import (
	"codearts2api/internal/provider"
	"testing"
)

func TestExplicitContentBlockClassification(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		blocked bool
	}{
		{400, `{"code":"content_policy_violation","msg":"policy"}`, true},
		{200, `{"ResponseMetadata":{"Error":{"Code":"content_filter","Message":"policy"}}}`, true},
		{400, `{"code":11101,"msg":"blocked by security policy"}`, true},
		{401, `{"code":"content_filter","msg":"blocked by security policy"}`, false},
		{401, `{"code":11101,"msg":"blocked by security policy"}`, false},
		{400, `{"code":9074,"msg":"blocked by security policy"}`, false},
		{403, `{"code":2001,"msg":"blocked by security policy"}`, false},
		{403, `{"code":9074,"msg":"request illegal"}`, false},
		{400, `{"code":11128,"msg":"request illegal"}`, false},
		{403, `{"code":2001,"msg":"permission denied"}`, false},
	} {
		err := errorFrom(tc.status, []byte(tc.body), false)
		if err == nil {
			t.Fatal("missing error")
		}
		e := err.(*provider.Error)
		if e.ContentBlocked != tc.blocked || tc.blocked && e.AccountFault {
			t.Fatalf("classification %+v", e)
		}
	}
}
