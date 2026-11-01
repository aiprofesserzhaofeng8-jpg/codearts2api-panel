package codearts

import "testing"

func TestPromptBlockClassification(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		blocked bool
	}{
		{400, `{"error":{"code":"content_policy_violation","message":"policy"}}`, true},
		{403, `{"error_code":"policy","error_msg":"内容安全审核未通过"}`, true},
		{400, `content_filter: blocked by security policy`, true},
		{401, `blocked by security policy`, false},
		{403, `{"error_code":"permission","error_msg":"not authorized"}`, false},
		{400, `{"error_code":"TM.00001041","error_msg":"concurrency limit"}`, false},
	} {
		err := &UpstreamError{Status: tc.status, Message: tc.body}
		if err.PromptBlocked() != tc.blocked {
			t.Fatalf("classification %+v", err)
		}
	}
}

func TestBusinessContentCodeSurvivesStreamingConversion(t *testing.T) {
	msg, status, bad := IsUpstreamBusinessError([]byte(`{"error_code":"content_filter","error_msg":"policy"}`))
	if !bad || !(&UpstreamError{Status: status, Message: msg}).PromptBlocked() {
		t.Fatal("business code lost during SSE conversion")
	}
	if !(&UpstreamError{Status: 400, Message: `{"error_code":"content_filter","error_msg":"policy"}`}).PromptBlocked() {
		t.Fatal("flat policy code not classified")
	}
}
