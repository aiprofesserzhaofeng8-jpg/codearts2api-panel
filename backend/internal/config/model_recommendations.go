package config

import "codearts2api/internal/provider"

type ModelPolicyStore interface {
	ModelPolicy(string) (ModelPolicy, bool)
	ModelPolicies() map[string]ModelPolicy
}

type ModelPolicyDirectory interface {
	List(map[string]string) []provider.Model
}

// ModelPolicies borrows current configured policies and the single directory.
// Recommendations never infer native capabilities from a model's name.
type ModelPolicies struct {
	store     ModelPolicyStore
	directory ModelPolicyDirectory
}

func NewModelPolicies(store ModelPolicyStore, directory ModelPolicyDirectory) *ModelPolicies {
	return &ModelPolicies{store: store, directory: directory}
}

func (s *ModelPolicies) For(m provider.Model) (ModelPolicy, bool) {
	if p, ok := s.store.ModelPolicy(m.ID); ok {
		return p, true
	}
	if m.Provider == "" || m.Provider == provider.CodeArts || m.Context < 8192 || m.MaxOutput < 256 {
		return ModelPolicy{}, false
	}
	window := min(m.Context, 2097152)
	output := min(m.MaxOutput, 8192, window/4)
	reserve := min(max(16384, output+4096), window-2048)
	trigger := min(45000, window-reserve)
	keep := min(12000, trigger/2)
	p := ModelPolicy{ContextWindow: window, MaxOutputTokens: output, ReserveTokens: reserve, KeepRecentTokens: keep, TriggerTokens: trigger, SummaryMaxTokens: output, Source: "基于上游目录的保守建议；此上游未启用服务端摘要适配"}
	return p, p.Validate() == nil
}

func (s *ModelPolicies) All() map[string]ModelPolicy {
	out := s.store.ModelPolicies()
	for _, m := range s.directory.List(nil) {
		if p, ok := s.For(m); ok {
			out[m.ID] = p
		}
	}
	return out
}

// CommonModelPolicy reads each source exactly once in order and stops at the
// first unavailable policy. Mapping/permission resolution belongs to its caller.
func CommonModelPolicy(ids []string, lookup func(string) (ModelPolicy, bool)) (ModelPolicy, bool) {
	if len(ids) == 0 {
		return ModelPolicy{}, false
	}
	var combined ModelPolicy
	for i, id := range ids {
		p, ok := lookup(id)
		if !ok {
			return ModelPolicy{}, false
		}
		if i == 0 {
			combined = p
		} else {
			combined.ContextWindow = min(combined.ContextWindow, p.ContextWindow)
			combined.MaxOutputTokens = min(combined.MaxOutputTokens, p.MaxOutputTokens)
			combined.TriggerTokens = min(combined.TriggerTokens, p.TriggerTokens)
			combined.KeepRecentTokens = min(combined.KeepRecentTokens, p.KeepRecentTokens)
			combined.ReserveTokens = max(combined.ReserveTokens, p.ReserveTokens)
		}
	}
	combined.ReserveTokens = min(combined.ReserveTokens, combined.ContextWindow-combined.TriggerTokens)
	combined.SummaryEnabled, combined.SummaryRetry = false, false
	combined.SummaryFallbackModel = ""
	combined.SummaryMaxTokens = combined.MaxOutputTokens
	combined.Source = "分组内各来源的保守公共建议；实际能力以所选上游为准"
	return combined, combined.Validate() == nil
}
