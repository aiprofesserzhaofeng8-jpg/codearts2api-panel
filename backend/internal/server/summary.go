package server

import (
	"context"
	"encoding/json"
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

// The gateway owns private buffering and bounded retries. Account policy,
// fallback authorization, attempt accounting and cooldown writes stay here.
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
	target, policy := model, p
	if retry && p.SummaryFallbackModel != "" {
		if forced := s.Store.Settings().ForceModel; forced != "" && forced != p.SummaryFallbackModel {
			return gateway.SummaryAttempt{}, &gateway.SummaryFailure{Message: "强制模型设置阻止摘要备用模型调用"}
		}
		var exists bool
		target, exists = s.Reg.Resolve(p.SummaryFallbackModel, nil)
		if !exists || modelProvider(target) != modelProvider(model) || target.Scope != model.Scope {
			return gateway.SummaryAttempt{}, &gateway.SummaryFailure{Message: "摘要备用模型已不在模型目录中"}
		}
		if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
			if s.Store.ModelHealthSnapshot()[c.accountID][target.ID].CooldownUntil > time.Now().Unix() {
				return gateway.SummaryAttempt{}, &gateway.SummaryFailure{Message: "摘要备用模型正在冷却，请调整策略或稍后再试"}
			}
		}
		if configured, exists := s.Store.ModelPolicy(target.ID); exists {
			policy = configured
		} else {
			policy.DisableSummaryThinking = false
		}
	}
	if retry {
		if err := s.checkRequestBudget(ctx, target.ID); err != nil {
			return gateway.SummaryAttempt{}, err
		}
	}
	requestBody, err := rewriteModel(body, target.ID, true)
	if err != nil {
		return gateway.SummaryAttempt{}, err
	}
	if target.ID != model.ID {
		// Reasoning options for the original model are not a capability claim
		// about the explicitly selected fallback.
		var request map[string]json.RawMessage
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return gateway.SummaryAttempt{}, err
		}
		delete(request, "thinking")
		delete(request, "reasoning_effort")
		requestBody = mustJSON(request)
	}
	request, err := requestpolicy.PrepareSummary(requestBody, requestpolicy.SummaryOptions{MaxTokens: policy.SummaryMaxTokens, DisableThinking: policy.DisableSummaryThinking}, retry)
	if err != nil {
		return gateway.SummaryAttempt{}, err
	}
	return gateway.SummaryAttempt{Model: target.ID, Body: request}, nil
}
