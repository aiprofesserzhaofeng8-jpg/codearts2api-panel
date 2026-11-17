// Package upstream binds existing platform transports to selected attempts.
// Concrete protocols stay in their own platform packages.
package upstream

import (
	"context"
	"encoding/json"

	"codearts2api/internal/provider"
)

// Streamer is the existing stream-only adapter contract, without its unrelated
// login, refresh or management capabilities.
type Streamer interface {
	Stream(context.Context, json.RawMessage, provider.Request, func([]byte) error) error
}

// StreamSession borrows resolved credentials and immutable request identifiers.
// Binding and forwarding do not normalize, copy or serialize credentials.
type StreamSession struct {
	inbound              provider.Protocol
	supported            []provider.Protocol
	adapter              Streamer
	credentials          json.RawMessage
	sessionID, requestID string
}

func BindStream(adapter Streamer, credentials json.RawMessage, sessionID, requestID string) StreamSession {
	return StreamSession{adapter: adapter, credentials: credentials, sessionID: sessionID, requestID: requestID}
}

func (s StreamSession) Stream(ctx context.Context, model string, body []byte, onChunk func([]byte) error) error {
	_, _, upstreamModel := provider.ParseModel(model)
	return s.adapter.Stream(ctx, s.credentials, provider.Request{
		Model: upstreamModel, Body: body, SupportedProtocols: s.supported, InboundProtocol: s.inbound, SessionID: s.sessionID, RequestID: s.requestID,
	}, onChunk)
}

func (s StreamSession) WithModel(model provider.Model) StreamSession {
	s.supported = model.SupportedProtocols
	return s
}

func (s StreamSession) WithProtocol(p provider.Protocol) StreamSession { s.inbound = p; return s }

// NativeSession binds the protocol-aware optional transport to the same gateway
// single-attempt executor used by legacy streams.
type NativeSession struct {
	adapter     provider.NativeStreaming
	credentials json.RawMessage
	request     provider.Request
}

func BindNative(adapter provider.NativeStreaming, credentials json.RawMessage, request provider.Request) NativeSession {
	return NativeSession{adapter, credentials, request}
}
func (s NativeSession) Stream(ctx context.Context, _ string, body []byte, emit func([]byte) error) error {
	request := s.request
	request.Body = body
	return s.adapter.StreamNative(ctx, s.credentials, request, emit)
}
