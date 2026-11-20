package cline

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitErrorsAreModelScopedAndSanitized(t *testing.T) {
	for _, tc := range []struct {
		body, code string
		status     int
	}{
		{`{"error":{"code":"insufficient_credits","metadata":{"secret":"private"}}}`, "INSUFFICIENT_CREDITS", 429},
		{`{"error":{"details":{"code":"SPEND_LIMIT_EXCEEDED"}}}`, "SPEND_LIMIT_EXCEEDED", 429},
		{`{"error":{"code":"INFERENCE_CAP_ERROR"}}`, "INFERENCE_CAP_ERROR", 429},
		{`{"error":{"message":"You have reached your weekly ClinePass limit. Please try again later."}}`, "CLINE_PASS_LIMIT", 429},
		{`{"error":{"message":"the user is not subscribed to required model plan"}}`, "CLINE_PASS_UNAVAILABLE", 403},
		{`{"error":{"message":"Free limit reached on model X"}}`, "CLINE_FREE_LIMIT", 429},
	} {
		e := classifyFailure([]byte(tc.body), providerhttp.Failure("Cline", 402)).(*provider.Error)
		if e.AccountFault || e.Code != tc.code || e.Status != tc.status || strings.Contains(e.Message, "private") || e.RetryAt != 0 {
			t.Fatal(e)
		}
	}
	e := classifyFailure([]byte(`{"error":{"metadata":{"code":"insufficient_credits"}}}`), providerhttp.Failure("Cline", 500)).(*provider.Error)
	if e.Code != "" || e.Status != 500 {
		t.Fatal("metadata classified", e)
	}
	e = classifyFailure([]byte(`{"error":{"code":"insufficient_credits"}}`), providerhttp.Failure("Cline", 401)).(*provider.Error)
	if !e.AccountFault || e.Status != 401 {
		t.Fatal("auth failure masked")
	}
}
func TestSSELimitAndReasoningPreservation(t *testing.T) {
	for _, failure := range []bool{true, false} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			if failure {
				io.WriteString(w, "data: {\"error\":{\"code\":\"INFERENCE_CAP_ERROR\"}}\n\n")
				return
			}
			io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\"thought\",\"reasoning_details\":[{\"type\":\"reasoning.encrypted\",\"data\":\"opaque\"}]},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}))
		c := New()
		c.Origin = s.URL
		var out strings.Builder
		err := c.Stream(context.Background(), rawKey(), provider.Request{Model: "cline-pass/test", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func(b []byte) error { out.Write(b); return nil })
		if failure {
			var e *provider.Error
			if !errors.As(err, &e) || e.Status != 429 || e.AccountFault {
				t.Fatal(err)
			}
		} else if err != nil || !strings.Contains(out.String(), "opaque") || !strings.Contains(out.String(), "thought") || !strings.Contains(out.String(), "reasoning_content") {
			t.Fatal(err, out.String())
		}
		s.Close()
	}
}

func TestDocumentedErrorFinishIsNotSuccess(t *testing.T) {
	_, err := normalizeReasoning([]byte(`{"choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":"error"}]}`))
	if err == nil {
		t.Fatal("error finish accepted")
	}
}

func TestErrorFinishAfterPartialOutputFailsRequest(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"error\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer s.Close()
	c := New()
	c.Origin = s.URL
	frames := 0
	err := c.Stream(context.Background(), rawKey(), provider.Request{Model: "cline-pass/test", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { frames++; return nil })
	if err == nil || frames != 1 {
		t.Fatal("error became completed response", err, frames)
	}
}
