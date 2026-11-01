package gateway

import (
	"context"
	"errors"

	"codearts2api/internal/provider"
	"codearts2api/internal/requestpolicy"
)

// PromptPolicy is the inference-only projection of one platform's settings.
// It does not contain storage, account or search configuration.
type PromptPolicy struct {
	Mode, Text         string
	Fallback, Sanitize bool
}

// PromptRecovery retains the caller's attempt accounting and budget boundary.
// Both hooks are required when fallback is enabled. BeforeRetry finishes the
// blocked attempt and checks the existing budget; StartRetry runs only after
// the replacement body is prepared. Neither hook acquires another account.
type PromptRecovery struct {
	BeforeRetry func(error) error
	StartRetry  func()
}

// PromptBlocked preserves platform error identity, including wrapped errors.
func PromptBlocked(err error) bool {
	var blocked interface{ PromptBlocked() bool }
	return errors.As(err, &blocked) && blocked.PromptBlocked()
}

// RunPromptPolicy applies policy to one bound attempt, with at most one neutral
// prompt recovery. Any observed upstream frame closes the recovery window,
// including empty or usage-only frames retained by a non-streaming consumer.
// The run callback must retain the selected model, credentials and account lease.
func RunPromptPolicy(ctx context.Context, body []byte, policy PromptPolicy, recovery PromptRecovery, run func([]byte, func()) error) error {
	prepared, err := requestpolicy.PrepareBody(body, policy.Mode, policy.Text, policy.Sanitize)
	if err != nil {
		return &provider.Error{Status: 400, Message: err.Error()}
	}
	emitted := false
	observe := func() { emitted = true }
	err = run(prepared, observe)
	if policy.Mode != "passthrough" || !policy.Fallback || emitted || !PromptBlocked(err) || ctx.Err() != nil {
		return err
	}
	if err = recovery.BeforeRetry(err); err != nil {
		return err
	}
	prepared, err = requestpolicy.PrepareBody(body, "custom", requestpolicy.FallbackSystemPrompt, policy.Sanitize)
	if err != nil {
		return &provider.Error{Status: 400, Message: err.Error()}
	}
	recovery.StartRetry()
	return run(prepared, observe)
}
