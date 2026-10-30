package provider

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
)

// TransportError keeps the cause for classification, but never exposes its URL,
// proxy credentials or arbitrary error text in the public message.
type TransportError struct {
	service string
	kind    string
	cause   error
}

func NewTransportError(service string, cause error) *TransportError {
	kind := "connection_error"
	var dns *net.DNSError
	var op *net.OpError
	var network net.Error
	switch {
	case errors.Is(cause, context.Canceled):
		kind = "canceled"
	case errors.Is(cause, context.DeadlineExceeded):
		kind = "timeout"
	case errors.As(cause, &network) && network.Timeout():
		kind = "timeout"
		if strings.Contains(cause.Error(), "timeout awaiting response headers") {
			kind = "response_header_timeout"
		} else if errors.As(cause, &op) && op.Op == "dial" {
			kind = "connect_timeout"
		}
	case errors.As(cause, &dns):
		kind = "dns_error"
	case errors.Is(cause, syscall.ECONNREFUSED):
		kind = "connection_refused"
	case errors.Is(cause, syscall.ECONNRESET):
		kind = "connection_reset"
	}
	return &TransportError{service: service, kind: kind, cause: cause}
}

func (e *TransportError) Error() string       { return e.service + " 网络请求失败（" + e.kind + "）" }
func (e *TransportError) Unwrap() error       { return e.cause }
func (e *TransportError) FailureKind() string { return e.kind }
