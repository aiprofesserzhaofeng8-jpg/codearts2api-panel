// Package gateway owns request execution independently of HTTP response writing.
package gateway

import (
	"context"
	"fmt"

	"codearts2api/internal/protocol"
	"codearts2api/internal/provider"
)

// StreamTransport is the minimal inference contract. Stream-only platforms do
// not need to implement a synthetic non-streaming operation or fallback rule.
type StreamTransport interface {
	Stream(context.Context, string, []byte, func([]byte) error) error
}

// ChatSession adds native non-streaming support to a bound transport.
// Neither contract selects accounts, refreshes, applies policy or retries.
type ChatSession interface {
	StreamTransport
	Chat(context.Context, string, []byte) ([]byte, error)
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
	return e.collectStream(ctx, session, model, streamBody, observer, 0)
}

// CompleteStream keeps the existing 32 MiB aggregate limit of stream-only
// providers. Native Chat fallback retains its original independent behavior.
func (e *Completions) CompleteStream(ctx context.Context, session StreamTransport, model string, body []byte, observer Observer) ([]byte, error) {
	return e.collectStream(ctx, session, model, body, observer, 32<<20)
}

func (e *Completions) collectStream(ctx context.Context, session StreamTransport, model string, body []byte, observer Observer, limit int) ([]byte, error) {
	var chunks [][]byte
	size := 0
	err := e.Stream(ctx, session, model, body, observer, func(chunk []byte) error {
		size += len(chunk)
		if limit > 0 && size > limit {
			return fmt.Errorf("上游响应超过 %d MiB", limit>>20)
		}
		chunks = append(chunks, append([]byte(nil), chunk...))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return protocol.AggregateSSE(chunks)
}

func (e *Completions) Stream(ctx context.Context, session StreamTransport, model string, body []byte, observer Observer, onChunk func([]byte) error) error {
	return session.Stream(ctx, model, body, func(chunk []byte) error {
		observer.Observe()
		observer.Usage(ctx, chunk)
		return onChunk(chunk)
	})
}
