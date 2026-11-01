package server

import (
	"context"

	"codearts2api/internal/accounts"
	"codearts2api/internal/gateway"
	"codearts2api/internal/registry"
	"codearts2api/internal/requestpolicy"
)

// The caller resolves credentials once and retains its account lease. Protocol
// adapters only see the prepared Chat body. A consumed chunk always closes the
// retry window, including chunks buffered by a non-streaming protocol adapter.
func (s *Server) runRequestPolicy(ctx context.Context, acct *accounts.Account, model registry.Model, body []byte, run func([]byte, func()) error) error {
	policy := s.Store.Settings().ProviderPolicy(acct.ProviderID())
	explicit, _ := ctx.Value(summaryIntentKey{}).(bool)
	if requestpolicy.IsSummaryRequest(body, explicit) {
		policy.PromptMode, policy.PromptFallback = "passthrough", false
	}
	return gateway.RunPromptPolicy(ctx, body, gateway.PromptPolicy{
		Mode: policy.PromptMode, Text: policy.PromptText, Fallback: policy.PromptFallback, Sanitize: policy.Sanitize,
	}, gateway.PromptRecovery{
		BeforeRetry: func(err error) error {
			finishAttempt(ctx, err)
			return s.checkRequestBudget(ctx, model.ID)
		},
		StartRetry: func() { startAttempt(ctx, acct, model.ID) },
	}, run)
}
