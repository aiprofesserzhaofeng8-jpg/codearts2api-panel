package provider

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

type headerTimeout struct{}

func (headerTimeout) Error() string   { return "net/http: timeout awaiting response headers" }
func (headerTimeout) Timeout() bool   { return true }
func (headerTimeout) Temporary() bool { return true }

func TestTransportErrorRetainsCauseWithoutExposingURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind string
	}{
		{"cancel", context.Canceled, "canceled"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"headers", headerTimeout{}, "response_header_timeout"},
		{"dial", &net.OpError{Op: "dial", Err: context.DeadlineExceeded}, "timeout"},
		{"dns", &net.DNSError{Err: "lookup failed", Name: "private-canary"}, "dns_error"},
		{"refused", syscall.ECONNREFUSED, "connection_refused"},
		{"reset", syscall.ECONNRESET, "connection_reset"},
		{"other", errors.New("private-canary"), "connection_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := &url.Error{Op: "Post", URL: "https://user:private-canary@example.invalid/?token=private-canary", Err: tc.err}
			err := NewTransportError("WorkBuddy", cause)
			if err.FailureKind() != tc.kind || !errors.Is(err, tc.err) {
				t.Fatalf("lost cause/category: %s %s", err, err.FailureKind())
			}
			if strings.Contains(err.Error(), "private-canary") || strings.Contains(err.Error(), "https:") {
				t.Fatal("public transport error exposes credentials or URL")
			}
		})
	}
}
