package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"codearts2api/internal/apierr"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
)

type usageSnapshot struct {
	Prompt        *int64 `json:"prompt_tokens"`
	Completion    *int64 `json:"completion_tokens"`
	CacheHit      int64  `json:"prompt_cache_hit_tokens"`
	PromptDetails struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionDetails struct {
		Reasoning int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type usageFrame struct {
	Usage   *usageSnapshot `json:"usage"`
	Choices []struct {
		Finish string `json:"finish_reason"`
	} `json:"choices"`
}

type initialUsageFrame struct {
	Usage   *usageSnapshot `json:"usage"`
	Choices []struct {
		Finish string     `json:"finish_reason"`
		Delta  firstDelta `json:"delta"`
	} `json:"choices"`
}

type firstDelta struct {
	Content   json.RawMessage `json:"content"`
	Reasoning json.RawMessage `json:"reasoning_content"`
	Alternate json.RawMessage `json:"reasoning"`
	Refusal   json.RawMessage `json:"refusal"`
	Tools     []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func hasDeltaValue(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && !bytes.Equal(raw, []byte(`""`)) && !bytes.Equal(raw, []byte("[]"))
}

func (d firstDelta) meaningful() bool {
	if hasDeltaValue(d.Content) || hasDeltaValue(d.Reasoning) || hasDeltaValue(d.Alternate) || hasDeltaValue(d.Refusal) {
		return true
	}
	for _, tool := range d.Tools {
		if tool.ID != "" || tool.Function.Name != "" || hasDeltaValue(tool.Function.Arguments) {
			return true
		}
	}
	return false
}

func configureInference(ctx context.Context, start time.Time, stream bool) {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		c.started, c.stream = start, stream
	}
}

// These timestamps describe the first meaningful upstream delta, including
// reasoning/refusal/tools. Buffered non-stream replies have no first-token time.
func captureFirstToken(c *usageCapture) {
	if c.attemptFirstMS != nil {
		return
	}
	now := time.Now()
	if !c.attemptStarted.IsZero() {
		n := max(0, now.Sub(c.attemptStarted).Milliseconds())
		c.attemptFirstMS = &n
	}
	if c.firstTokenMS == nil && !c.started.IsZero() {
		n := max(0, now.Sub(c.started).Milliseconds())
		c.firstTokenMS = &n
	}
}

func finishAttempt(ctx context.Context, err error) {
	c, ok := ctx.Value(usageContextKey{}).(*usageCapture)
	if !ok || c.attempts == 0 || c.attemptDurationMS != nil {
		return
	}
	n := max(0, time.Since(c.attemptStarted).Milliseconds())
	c.attemptDurationMS = &n
	c.attemptStatus = apierr.From(err).Status
	c.attemptErrorKind = failureKind(ctx, err)
}

func (c *usageCapture) attemptDetail() store.RequestAttempt {
	return store.RequestAttempt{Number: c.attempts, Account: c.account, Model: c.model, FinishReason: c.finish, Usage: c.usage,
		DurationMS: c.attemptDurationMS, FirstTokenMS: c.attemptFirstMS, Status: c.attemptStatus, ErrorKind: c.attemptErrorKind}
}

func failureKind(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	var classified interface{ FailureKind() string }
	if errors.As(err, &classified) {
		return classified.FailureKind()
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	if errors.Is(err, apierr.NoAccount) {
		return "no_account"
	}
	var upstream *provider.Error
	if errors.As(err, &upstream) && upstream.AccountFault {
		return "credentials"
	}
	if promptBlocked(err) {
		return "content_blocked"
	}
	status := apierr.From(err).Status
	if status == http.StatusTooManyRequests {
		return "rate_limit"
	}
	if status >= 400 && status < 500 {
		return "invalid_request"
	}
	return "upstream_error"
}

type inferenceRejection struct {
	message string
	status  int
}

func (e *inferenceRejection) Error() string { return e.message }
func (e *inferenceRejection) FailureKind() string {
	if e.status >= 500 {
		return "internal_error"
	}
	return "invalid_request"
}

func (s *Server) recordRejectedInference(r *http.Request, model string, stream bool, start time.Time, status int, message string) {
	s.record(r.Context(), extractKey(r), nil, model, stream, status, start, 0, 0, &inferenceRejection{message, status})
}
