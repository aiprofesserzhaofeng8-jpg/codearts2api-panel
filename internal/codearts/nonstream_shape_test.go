package codearts

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func shapeTestChat(t *testing.T, body string) ([]byte, error) {
	t.Helper()
	client := NewClient()
	client.HTTP = &http.Client{Transport: creditRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return creditResp(r, body), nil
	})}
	return client.Chat(t.Context(), &Session{AccessKey: "shape-test-ak", SecretKey: "shape-test-sk"}, "test-model", []byte(`{"messages":[],"stream":false}`))
}

func TestNonStreamingChatRejectsInvalidEnvelope(t *testing.T) {
	for name, body := range map[string]string{
		"null":              `null`,
		"empty_object":      `{}`,
		"array":             `[]`,
		"number":            `42`,
		"string":            `"upstream problem"`,
		"missing_choices":   `{"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		"null_choices":      `{"choices":null}`,
		"empty_choices":     `{"choices":[],"usage":{"prompt_tokens":1}}`,
		"object_choices":    `{"choices":{}}`,
		"null_choice":       `{"choices":[null]}`,
		"missing_message":   `{"choices":[{"finish_reason":"stop"}]}`,
		"null_message":      `{"choices":[{"message":null}]}`,
		"array_message":     `{"choices":[{"message":[]}]}`,
		"scalar_message":    `{"choices":[{"message":"hello"}]}`,
		"invalid_second":    `{"choices":[{"message":{"content":"first"}},null]}`,
		"standard_error":    `{"error":{"type":"upstream_error","message":"PRIVATE_UPSTREAM_DETAIL"}}`,
		"string_error":      `{"error":"PRIVATE_UPSTREAM_DETAIL"}`,
		"mixed_error":       `{"choices":[{"message":{"content":"hello"}}],"error":{"message":"PRIVATE_UPSTREAM_DETAIL"}}`,
		"truncated_message": `{"choices":[{"message":`,
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := shapeTestChat(t, body)
			var upstream *UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != 502 || len(raw) != 0 {
				t.Fatalf("invalid envelope treated as success: body=%s raw=%s err=%v", body, raw, err)
			}
			if strings.Contains(err.Error(), "PRIVATE_UPSTREAM_DETAIL") {
				t.Fatal("malformed envelope leaked raw upstream details")
			}
		})
	}
}

func TestNonStreamingChatPreservesValidEnvelopeBytes(t *testing.T) {
	for name, body := range map[string]string{
		"text":                ` {"choices":[{"message":{"content":"11128 cc_version=42"},"finish_reason":"stop"}],"extension":9007199254740993} `,
		"tool_only":           `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"same","type":"function","function":{"name":"read","arguments":"{\"n\":9007199254740993}"}}]},"finish_reason":"tool_calls"}]}`,
		"refusal":             `{"choices":[{"message":{"content":null,"refusal":"cannot comply"},"finish_reason":"stop"}]}`,
		"reasoning":           `{"choices":[{"message":{"content":"","reasoning_content":"private reasoning"},"finish_reason":"stop"}]}`,
		"length":              `{"choices":[{"message":{"content":"partial answer"},"finish_reason":"length"}]}`,
		"future_finish":       `{"choices":[{"message":{"provider_extension":{"value":9007199254740993}},"finish_reason":"future_reason"}]}`,
		"empty_message":       `{"choices":[{"message":{},"finish_reason":"stop"}]}`,
		"missing_finish":      `{"choices":[{"message":{"content":"hello"}}]}`,
		"content_array":       `{"choices":[{"message":{"content":[{"type":"future","blob":{"value":9007199254740993}}]}}]}`,
		"multiple":            `{"choices":[{"index":0,"message":{"content":"first"}},{"index":1,"message":{"content":"second"}}]}`,
		"null_error":          `{"error":null,"choices":[{"message":{"content":"hello"}}]}`,
		"empty_business_code": `{"error_code":"","error_msg":"content_policy_violation","choices":[{"message":{"content":"hello"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := shapeTestChat(t, body)
			if err != nil || string(raw) != body {
				t.Fatalf("valid bytes changed: raw=%s err=%v", raw, err)
			}
		})
	}
}

func TestNonStreamingBusinessErrorPrecedence(t *testing.T) {
	for _, suffix := range []string{
		``,
		`,"choices":null`,
		`,"choices":[]`,
		`,"choices":[{"message":{"content":"hello"}}]`,
		`,"error":{"status":401,"message":"invalid_grant"}`,
	} {
		raw, err := shapeTestChat(t, `{"error_code":"422","error_msg":"invalid request"`+suffix+`}`)
		var upstream *UpstreamError
		if len(raw) != 0 || !errors.As(err, &upstream) || upstream.Status != 422 || !IsClientRequestError(err) || upstream.PromptBlocked() {
			t.Fatalf("legacy business status lost priority: suffix=%s raw=%s err=%v", suffix, raw, err)
		}
	}
}

func TestNonStreamingChatPreservesBusinessErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		code, message string
		status        int
		blocked       bool
	}{
		{"400", "invalid request", 400, false},
		{"content_filter", "policy", 400, true},
		{"403", "permission denied", 403, false},
		{"429", "rate limit", 429, false},
		{"InferHub.failure.503", "temporarily unavailable", 503, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			body := `{"error_code":"` + tc.code + `","error_msg":"` + tc.message + `"}`
			raw, err := shapeTestChat(t, body)
			var upstream *UpstreamError
			if len(raw) != 0 || !errors.As(err, &upstream) || upstream.Status != tc.status || upstream.PromptBlocked() != tc.blocked {
				t.Fatalf("business classification changed: raw=%s err=%v", raw, err)
			}
		})
	}
}
