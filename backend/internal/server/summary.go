package server

import (
	"context"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/gateway"
	"codearts2api/internal/registry"
	"codearts2api/internal/requestpolicy"
	"codearts2api/internal/store"
)

func (s *Server) summaryPolicy(ctx context.Context, body []byte, model string) (store.ModelPolicy, bool) {
	explicit, _ := ctx.Value(summaryIntentKey{}).(bool)
	p, ok := s.Store.ModelPolicy(model)
	return p, ok && p.SummaryEnabled && requestpolicy.IsSummaryRequest(body, explicit)
}

// The gateway owns preparation, private buffering and bounded retries. HTTP
// context projection, attempt accounting and cooldown writes stay here.
func (s *Server) summaryChunks(ctx context.Context, sess *codearts.Session, model registry.Model, body []byte, p store.ModelPolicy) ([][]byte, error) {
	return gateway.RunSummary(ctx, s.CodeArts.Bind(sess), p.SummaryRetry, gateway.SummaryHooks{
		Prepare: func(retry bool) (gateway.SummaryAttempt, error) {
			return s.prepareSummaryAttempt(ctx, model, body, p, retry)
		},
		Start: func(number int, target string) {
			if number > 1 {
				resetUsage(ctx)
				if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
					c.model = target
					if c.mapping != nil {
						c.selectedModel = target
					}
				}
			}
			markSummary(ctx, number)
		},
		Usage: captureUsage,
		Result: func(target string, err error) {
			if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
				if err == nil {
					s.Store.MarkModelResult(c.accountID, target, true, "", 0)
				} else if codearts.IsTransientUpstream(err) || modelUnavailable(err) {
					s.Store.MarkModelResult(c.accountID, target, false, "摘要上游暂不可用", s.Store.Settings().Pool.RateCooldownSec)
				}
			}
		},
	})
}

func (s *Server) prepareSummaryAttempt(ctx context.Context, model registry.Model, body []byte, p store.ModelPolicy, retry bool) (gateway.SummaryAttempt, error) {
	return gateway.PrepareSummaryAttempt(ctx, summaryTarget(model), body, summaryOptions(p), p.SummaryFallbackModel, retry, summaryTargetSource{s, ctx})
}

type summaryTargetSource struct {
	s   *Server
	ctx context.Context
}

func summaryTarget(model registry.Model) gateway.SummaryTarget {
	return gateway.SummaryTarget{ID: model.ID, Provider: modelProvider(model), Scope: model.Scope}
}
func summaryOptions(p store.ModelPolicy) requestpolicy.SummaryOptions {
	return requestpolicy.SummaryOptions{MaxTokens: p.SummaryMaxTokens, DisableThinking: p.DisableSummaryThinking}
}
func (source summaryTargetSource) ForcedModel() string { return source.s.Store.Settings().ForceModel }
func (source summaryTargetSource) Resolve(name string) (gateway.SummaryTarget, bool) {
	model, ok := source.s.Reg.Resolve(name, nil)
	return summaryTarget(model), ok
}
func (source summaryTargetSource) Cooling(model string) bool {
	c, ok := source.ctx.Value(usageContextKey{}).(*usageCapture)
	return ok && source.s.Store.ModelHealthSnapshot()[c.accountID][model].CooldownUntil > time.Now().Unix()
}
func (source summaryTargetSource) Options(model string) (requestpolicy.SummaryOptions, bool) {
	p, ok := source.s.Store.ModelPolicy(model)
	return summaryOptions(p), ok
}
func (source summaryTargetSource) Authorize(ctx context.Context, model string) error {
	return source.s.checkRequestBudget(ctx, model)
}
