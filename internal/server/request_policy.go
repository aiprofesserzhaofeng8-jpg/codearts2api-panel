package server

import (
	"context"
	"errors"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/requestpolicy"
)

func promptBlocked(err error) bool {
	var blocked interface{ PromptBlocked() bool }
	return errors.As(err, &blocked) && blocked.PromptBlocked()
}

// The caller resolves credentials once and retains its account lease. Protocol
// adapters only see the prepared Chat body. A consumed chunk always closes the
// retry window, including chunks buffered by a non-streaming protocol adapter.
func (s *Server) runRequestPolicy(ctx context.Context, acct *codearts.Account, model registry.Model, body []byte, run func([]byte, func()) error) error {
	policy := s.Store.Settings().ProviderPolicy(acct.ProviderID())
	explicit, _ := ctx.Value(summaryIntentKey{}).(bool)
	if summaryRequest(body, explicit) {
		policy.PromptMode, policy.PromptFallback = "passthrough", false
	}
	prepared, err := requestpolicy.PrepareBody(body, policy.PromptMode, policy.PromptText, policy.Sanitize)
	if err != nil {
		return &provider.Error{Status: 400, Message: err.Error()}
	}
	emitted := false
	observe := func() { emitted = true }
	err = run(prepared, observe)
	if policy.PromptMode != "passthrough" || !policy.PromptFallback || emitted || !promptBlocked(err) || ctx.Err() != nil {
		return err
	}
	if err = s.checkRequestBudget(ctx, model.ID); err != nil {
		return err
	}
	prepared, err = requestpolicy.PrepareBody(body, "custom", requestpolicy.FallbackSystemPrompt, policy.Sanitize)
	if err != nil {
		return &provider.Error{Status: 400, Message: err.Error()}
	}
	startAttempt(ctx, acct, model.ID)
	return run(prepared, observe)
}
