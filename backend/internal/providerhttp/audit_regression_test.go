package providerhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"codearts2api/internal/provider"
)

type auditTransport struct{}

func (auditTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("fixture network outage")
}

func TestAuditSSESeparatesEarlyDisconnectFromBadProtocol(t *testing.T) {
	for _, wire := range []string{"", "data: partial"} {
		err := SSE(context.Background(), strings.NewReader(wire), "Fixture", func(string, []byte) error { return nil })
		var network *provider.TransportError
		if !errors.As(err, &network) || !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatal("early EOF lost transport category", err)
		}
	}
	err := SSE(context.Background(), strings.NewReader("data: done\n\n"), "Fixture", func(string, []byte) error { return End })
	if err != nil {
		t.Fatal("terminal event stopped working", err)
	}
}
func TestAuditSharedTransportKeepsNetworkClassification(t *testing.T) {
	_, err := Do(context.Background(), &http.Client{Transport: auditTransport{}}, "Fixture", http.MethodGet, "https://fixture.invalid/", nil, nil)
	var network *provider.TransportError
	if !errors.As(err, &network) {
		t.Fatal("network failure converted into upstream status", err)
	}
}
