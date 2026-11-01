package gateway

import (
	"context"

	"codearts2api/internal/requestpolicy"
)

// SummaryTarget identifies a concrete catalog model, not a public group alias.
// Preparing another target never acquires an account or replaces credentials.
type SummaryTarget struct {
	ID, Provider, Scope string
}

// SummaryTargetSource reads current policy at the same boundaries as the
// caller's original attempt. Authorize must check group permissions before
// budget progress, including usage already consumed by the first attempt.
type SummaryTargetSource interface {
	ForcedModel() string
	Resolve(string) (SummaryTarget, bool)
	Cooling(string) bool
	Options(string) (requestpolicy.SummaryOptions, bool)
	Authorize(context.Context, string) error
}

// PrepareSummaryAttempt resolves and authorizes a bounded summary replacement.
// The first attempt uses its admitted policy. A retry checks force, catalog,
// platform/region, cooldown and current permissions/budget before preparing any
// replacement body or starting its accounting. Same-model retries also recheck.
func PrepareSummaryAttempt(ctx context.Context, model SummaryTarget, body []byte, options requestpolicy.SummaryOptions, fallback string, retry bool, source SummaryTargetSource) (SummaryAttempt, error) {
	target := model
	if retry && fallback != "" {
		if forced := source.ForcedModel(); forced != "" && forced != fallback {
			return SummaryAttempt{}, &SummaryFailure{Message: "强制模型设置阻止摘要备用模型调用"}
		}
		var exists bool
		target, exists = source.Resolve(fallback)
		if !exists || target.Provider != model.Provider || target.Scope != model.Scope {
			return SummaryAttempt{}, &SummaryFailure{Message: "摘要备用模型已不在模型目录中"}
		}
		if source.Cooling(target.ID) {
			return SummaryAttempt{}, &SummaryFailure{Message: "摘要备用模型正在冷却，请调整策略或稍后再试"}
		}
		if configured, exists := source.Options(target.ID); exists {
			options = configured
		} else {
			options.DisableThinking = false
		}
	}
	if retry {
		if err := source.Authorize(ctx, target.ID); err != nil {
			return SummaryAttempt{}, err
		}
	}
	request, err := requestpolicy.PrepareSummaryModel(body, target.ID, options, retry, target.ID != model.ID)
	if err != nil {
		return SummaryAttempt{}, err
	}
	return SummaryAttempt{Model: target.ID, Body: request}, nil
}
