// Package gateway owns request execution independently of HTTP response writing.
package gateway

import (
	"context"

	"codearts2api/internal/protocol"
	"codearts2api/internal/provider"
)

// ChatSession is a platform transport bound to an immutable credential generation.
// It does not select accounts, refresh, apply policy, retry or settle usage.
type ChatSession interface {
	Chat(context.Context, string, []byte) ([]byte, error)
	Stream(context.Context, string, []byte, func([]byte) error) error
	NeedsStreamFallback(error) bool
}

// Observer keeps the current output and usage accounting boundaries explicit.
// Buffered chunks still count as observed output and close the policy retry window.
type Observer struct {
	Observe func()
	Usage   func(context.Context, []byte)
}

// Completions executes one already-selected, already-authorized attempt.
// Account admission, prompt recovery and summaries retain their existing owner
// during M3; none of them may be repeated by this executor.
type Completions struct {
	MarkStreamOnly func(string)
}

func (e *Completions) Complete(ctx context.Context, session ChatSession, model provider.Model, body []byte, observer Observer) ([]byte, error) {
	if model.StreamOnly {
		return e.aggregate(ctx, session, model.ID, body, observer)
	}
	raw, err := session.Chat(ctx, model.ID, body)
	if err != nil {
		if session.NeedsStreamFallback(err) {
			e.MarkStreamOnly(model.ID)
			return e.aggregate(ctx, session, model.ID, body, observer)
		}
		return nil, err
	}
	observer.Usage(ctx, raw)
	return raw, nil
}

func (e *Completions) aggregate(ctx context.Context, session ChatSession, model string, body []byte, observer Observer) ([]byte, error) {
	streamBody, err := protocol.RewriteStream(body, true)
	if err == nil {
		streamBody, err = protocol.RequestStreamUsage(streamBody)
	}
	if err != nil {
		return nil, err
	}
	var chunks [][]byte
	err = e.Stream(ctx, session, model, streamBody, observer, func(chunk []byte) error {
		chunks = append(chunks, append([]byte(nil), chunk...))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return protocol.AggregateSSE(chunks)
}

func (e *Completions) Stream(ctx context.Context, session ChatSession, model string, body []byte, observer Observer, onChunk func([]byte) error) error {
	return session.Stream(ctx, model, body, func(chunk []byte) error {
		observer.Observe()
		observer.Usage(ctx, chunk)
		return onChunk(chunk)
	})
}
