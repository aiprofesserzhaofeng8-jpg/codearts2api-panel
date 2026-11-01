package server

import (
	"context"
	"net/http"

	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog"
	"codearts2api/internal/protocol"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// Per-request projection only; catalog owns resolution, Store owns admission.
type groupModelPlan = catalog.ModelPlan

func mappedPlan(ctx context.Context) *groupModelPlan {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.mapping
	}
	return nil
}

func (s *Server) prepareMappedAttempt(ctx context.Context, acct *accounts.Account, model registry.Model, body []byte) (registry.Model, []byte, error) {
	plan := mappedPlan(ctx)
	if plan == nil {
		return model, body, nil
	}
	id, _ := ctx.Value(routingModelKey{}).(string)
	actual, err := plan.Selected(id, acct.ProviderID())
	if err != nil {
		return model, nil, err
	}
	raw, err := protocol.MappedRequestModel(body, actual.ID)
	return actual, raw, err
}

func filterMappedMessages(ctx context.Context, thinking bool) error {
	return mappedPlan(ctx).FilterMessages(thinking)
}

func (s *Server) requestModelPolicy(r *http.Request, model registry.Model) (store.ModelPolicy, bool) {
	return s.Routing.ModelPolicy(groupKey(r.Context()), model, s.ModelPolicies)
}

// Existing same-platform summary fallback remains subject to the raw allowlist.
func (s *Server) mappedSummaryFallback(ctx context.Context, target string) bool {
	if requestKind(ctx) != "summary" || summaryAttempts(ctx) == 0 {
		return false
	}
	base, _ := ctx.Value(routingModelKey{}).(string)
	p, ok := s.Store.ModelPolicy(base)
	return ok && p.SummaryRetry && p.SummaryFallbackModel == target
}

// Only mapped Chat responses need this envelope projection.
func mappedResponseModel(raw []byte, name string) ([]byte, error) {
	return protocol.MappedResponseModel(raw, name)
}
