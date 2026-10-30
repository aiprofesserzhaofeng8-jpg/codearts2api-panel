package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Client recommendations and gateway behavior; never infer native capabilities
// from a model's marketing name or from a user's chosen context value.
type ModelPolicy struct {
	SummaryFallbackModel   string `json:"summary_fallback_model,omitempty"`
	ContextWindow          int    `json:"context_window"`
	MaxOutputTokens        int    `json:"max_output_tokens"`
	ReserveTokens          int    `json:"reserve_tokens"`
	KeepRecentTokens       int    `json:"keep_recent_tokens"`
	TriggerTokens          int    `json:"trigger_tokens"`
	SummaryEnabled         bool   `json:"summary_enabled"`
	SummaryRetry           bool   `json:"summary_retry"`
	SummaryMaxTokens       int    `json:"summary_max_tokens"`
	DisableSummaryThinking bool   `json:"disable_summary_thinking"`
	Source                 string `json:"source"`
}

func DefaultModelPolicies() map[string]ModelPolicy {
	return map[string]ModelPolicy{"deepseek-v4.1-flash": {
		ContextWindow: 65536, MaxOutputTokens: 8192, ReserveTokens: 16384, KeepRecentTokens: 12000, TriggerTokens: 45000,
		SummaryEnabled: true, SummaryRetry: true, SummaryMaxTokens: 8192, DisableSummaryThinking: true,
		Source: "保守推荐；已验证 65918 输入 Token，未验证上游最大上下文",
	}, "trae/cn/deepseek-v4.1-flash": {
		ContextWindow: 65536, MaxOutputTokens: 4096, ReserveTokens: 16384, KeepRecentTokens: 12000, TriggerTokens: 45000,
		SummaryMaxTokens: 4096, Source: "Trae 客户端保守推荐，非上游最大能力声明",
	}}
}
func (p ModelPolicy) Validate() error {
	if p.ContextWindow < 8192 || p.ContextWindow > 2097152 {
		return fmt.Errorf("上下文推荐值须在 8192–2097152 之间")
	}
	if p.MaxOutputTokens < 256 || p.MaxOutputTokens >= p.ContextWindow {
		return fmt.Errorf("输出上限须至少 256 且小于上下文")
	}
	if p.ReserveTokens < p.MaxOutputTokens+4096 || p.ReserveTokens >= p.ContextWindow {
		return fmt.Errorf("压缩预留须至少为输出上限 + 4096，且小于上下文")
	}
	if p.TriggerTokens < 1024 || p.TriggerTokens > p.ContextWindow-p.ReserveTokens {
		return fmt.Errorf("压缩触发值须在 1024 到上下文减预留之间")
	}
	if p.KeepRecentTokens < 512 || p.KeepRecentTokens >= p.TriggerTokens {
		return fmt.Errorf("保留历史须在 512 到压缩触发值之间")
	}
	if p.SummaryMaxTokens < 256 || p.SummaryMaxTokens > p.MaxOutputTokens {
		return fmt.Errorf("摘要输出预算须在 256 到模型输出上限之间")
	}
	if len(p.SummaryFallbackModel) > 200 {
		return fmt.Errorf("摘要备用模型 ID 过长")
	}
	if len(p.Source) > 500 {
		return fmt.Errorf("来源说明过长")
	}
	return nil
}
func (s *Store) ModelPolicies() map[string]ModelPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := DefaultModelPolicies()
	for k, v := range s.st.ModelPolicies {
		out[k] = v
	}
	return out
}
func (s *Store) ModelPolicy(model string) (ModelPolicy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.st.ModelPolicies[model]
	if !ok {
		p, ok = DefaultModelPolicies()[model]
	}
	return p, ok
}
func (s *Store) SetModelPolicyAndSave(model string, p ModelPolicy) error {
	model = strings.TrimSpace(model)
	if model == "" || len(model) > 200 {
		return fmt.Errorf("模型 ID 无效")
	}
	if err := p.Validate(); err != nil {
		return err
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.st.ModelPolicies
	next := make(map[string]ModelPolicy, len(before)+1)
	for k, v := range before {
		next[k] = v
	}
	next[model] = p
	s.st.ModelPolicies = next
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err == nil {
		err = s.writeState(raw)
	}
	if err != nil {
		s.st.ModelPolicies = before
	}
	return err
}
