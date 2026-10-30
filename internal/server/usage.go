package server

import (
	"codearts2api/internal/codearts"
	"codearts2api/internal/store"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type usageContextKey struct{}
type usageCapture struct {
	started           time.Time
	stream            bool
	firstTokenMS      *int64
	attemptStarted    time.Time
	attemptDurationMS *int64
	attemptFirstMS    *int64
	attemptStatus     int
	upstreamStatus    int
	attemptErrorKind  string
	attemptObserved   bool
	accountID         string
	details           []store.RequestAttempt
	account           string
	model             string
	usage             store.TokenUsage
	protocol          string
	id                string
	finish            string
	kind              string
	attempts          int
	summaryAttempts   int
	previous          store.TokenUsage
}

func withUsageCapture(ctx context.Context, protocol string) context.Context {
	return context.WithValue(ctx, usageContextKey{}, &usageCapture{protocol: protocol, id: codearts.RandomHex(12), kind: "inference"})
}
func resetUsage(ctx context.Context) {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		if c.attempts > 0 {
			finishAttempt(ctx, nil)
			c.details = append(c.details, c.attemptDetail())
		}
		addUsage(&c.previous, c.usage)
		c.usage = store.TokenUsage{}
		c.finish = ""
		c.attempts++
		c.attemptStarted = time.Now()
		c.attemptDurationMS, c.attemptFirstMS = nil, nil
		c.attemptStatus, c.attemptErrorKind = 0, ""
		c.upstreamStatus = 0
		c.attemptObserved = false
	}
}

// Chat streaming usage is a cumulative snapshot, not a per-chunk delta.
// Cached input tokens are a subset of prompt_tokens; never add them twice.
func captureUsage(ctx context.Context, raw []byte) {
	c, ok := ctx.Value(usageContextKey{}).(*usageCapture)
	if !ok {
		return
	}
	c.attemptObserved = true
	// Parse finish and usage together. Only until the first useful delta of an
	// attempt do we inspect delta fields; later chunks retain the small decoder.
	var u *usageSnapshot
	if c.stream && c.attemptFirstMS == nil {
		var frame initialUsageFrame
		if json.Unmarshal(raw, &frame) != nil {
			return
		}
		for _, choice := range frame.Choices {
			if choice.Finish != "" {
				c.finish = choice.Finish
			}
			if choice.Delta.meaningful() {
				captureFirstToken(c)
			}
		}
		u = frame.Usage
	} else {
		var frame usageFrame
		if json.Unmarshal(raw, &frame) != nil {
			return
		}
		for _, choice := range frame.Choices {
			if choice.Finish != "" {
				c.finish = choice.Finish
			}
		}
		u = frame.Usage
	}
	if u == nil || u.Prompt == nil || u.Completion == nil {
		return
	}
	cached := u.PromptDetails.Cached
	if cached == 0 {
		cached = u.CacheHit
	}
	c.usage = store.TokenUsage{Known: true, InputTokens: max(0, *u.Prompt), OutputTokens: max(0, *u.Completion), CachedTokens: min(max(0, cached), max(0, *u.Prompt)), ReasoningTokens: min(max(0, u.CompletionDetails.Reasoning), max(0, *u.Completion))}
}
func requestUsage(ctx context.Context) store.TokenUsage {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		total := c.previous
		addUsage(&total, c.usage)
		return total
	}
	return store.TokenUsage{}
}

func requestProtocol(ctx context.Context) string {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.protocol
	}
	return ""
}

// Ask the upstream for the final usage frame while preserving other options.
func requestStreamUsage(body []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	var options map[string]json.RawMessage
	if raw := m["stream_options"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &options); err != nil {
			return nil, err
		}
	}
	if options == nil {
		options = make(map[string]json.RawMessage)
	}
	options["include_usage"] = json.RawMessage("true")
	m["stream_options"], _ = json.Marshal(options)
	return json.Marshal(m)
}

func addUsage(total *store.TokenUsage, u store.TokenUsage) {
	if !u.Known {
		return
	}
	total.Known = true
	total.InputTokens += u.InputTokens
	total.OutputTokens += u.OutputTokens
	total.CachedTokens += u.CachedTokens
	total.ReasoningTokens += u.ReasoningTokens
}
func beginInference(w http.ResponseWriter, r *http.Request, protocol string) *http.Request {
	ctx := withUsageCapture(r.Context(), protocol)
	if r.Header.Get("X-CodeArts-Request-Kind") == "summary" {
		ctx = context.WithValue(ctx, summaryIntentKey{}, true)
	}
	w.Header().Set("X-Request-ID", requestID(ctx))
	return r.WithContext(ctx)
}
func requestID(ctx context.Context) string {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.id
	}
	return ""
}
func requestFinish(ctx context.Context) string {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.finish
	}
	return ""
}
func requestKind(ctx context.Context) string {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.kind
	}
	return "inference"
}
func requestAttempts(ctx context.Context) int {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.attempts
	}
	return 0
}
func summaryAttempts(ctx context.Context) int {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.summaryAttempts
	}
	return 0
}
func markSummary(ctx context.Context, attempt int) {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		c.kind = "summary"
		c.summaryAttempts = attempt
	}
}

func startAttempt(ctx context.Context, acct *codearts.Account, model string) {
	resetUsage(ctx)
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		c.account = acct.DisplayName()
		c.accountID = acct.ID
		c.model = model
	}
}
func requestAttemptDetails(ctx context.Context) []store.RequestAttempt {
	c, ok := ctx.Value(usageContextKey{}).(*usageCapture)
	if !ok || c.attempts == 0 {
		return nil
	}
	out := append([]store.RequestAttempt(nil), c.details...)
	return append(out, c.attemptDetail())
}

func currentAttemptUsage(ctx context.Context) store.TokenUsage {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.usage
	}
	return store.TokenUsage{}
}

func requestUsageIncomplete(ctx context.Context) bool {
	for _, a := range requestAttemptDetails(ctx) {
		if !a.Usage.Known {
			return true
		}
	}
	return false
}
