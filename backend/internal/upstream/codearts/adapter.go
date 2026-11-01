// Package codearts exposes the existing signed client as a native Chat transport.
// Credentials stay as immutable platform pointers; no JSON codec runs here.
package codearts

import (
	"context"

	legacy "codearts2api/internal/codearts"
)

type Adapter struct{ client *legacy.Client }

func New(client *legacy.Client) *Adapter { return &Adapter{client: client} }

// Session borrows an immutable credential generation for one selected attempt.
// The caller retains its account lease and controls refresh, policy and retries.
type Session struct {
	client      *legacy.Client
	credentials *legacy.Session
}

func (a *Adapter) Bind(credentials *legacy.Session) Session {
	return Session{client: a.client, credentials: credentials}
}

func (s Session) Chat(ctx context.Context, model string, body []byte) ([]byte, error) {
	return s.client.Chat(ctx, s.credentials, model, body)
}

func (s Session) Stream(ctx context.Context, model string, body []byte, onChunk func([]byte) error) error {
	return s.client.ChatStream(ctx, s.credentials, model, body, func(chunk []byte) error {
		if message, status, bad := legacy.IsUpstreamBusinessError(chunk); bad {
			return legacy.UpstreamBusinessError(status, message)
		}
		return onChunk(chunk)
	})
}

func (Session) NeedsStreamFallback(err error) bool { return legacy.IsEmptyRejection(err) }
