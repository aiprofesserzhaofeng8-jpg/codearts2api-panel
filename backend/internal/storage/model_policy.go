package storage

import (
	"encoding/json"
	"fmt"
	"strings"

	"codearts2api/internal/config"
)

type ModelPolicy = config.ModelPolicy

func DefaultModelPolicies() map[string]ModelPolicy { return config.DefaultModelPolicies() }

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
	return s.setModelPolicy(model, p, 0, false)
}
func (s *Store) SetModelPolicyWithSources(model string, p ModelPolicy, expected uint64, policies map[string]ModelPolicy) error {
	return s.setModelPolicy(model, p, expected, true, policies)
}
func (s *Store) setModelPolicy(model string, p ModelPolicy, expected uint64, strict bool, sources ...map[string]ModelPolicy) error {
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
	if strict {
		if err := checkRevision(expected, s.st.ModelRoutingRevision); err != nil {
			return err
		}
	}
	if strings.HasPrefix(model, "unified/") {
		return config.ModelFailure(400, "invalid_model_definition", "请通过统一模型事务保存公共策略")
	}
	before := s.st.ModelPolicies
	next := make(map[string]ModelPolicy, len(before)+1)
	for k, v := range before {
		next[k] = v
	}
	next[model] = p
	for _, m := range s.st.UnifiedModels {
		affected := false
		for _, src := range m.Sources {
			affected = affected || config.RawSource(src.ID) == model
		}
		if !affected {
			continue
		}
		policy, ok := next[config.UnifiedPolicyKey(m.ID)]
		if !ok {
			continue
		}
		if err := config.ValidateUnifiedPolicy(policy, m.Sources, func(id string) (ModelPolicy, bool) {
			if q, ok := next[id]; ok {
				return q, true
			}
			if len(sources) > 0 {
				if q, ok := sources[0][id]; ok {
					return q, true
				}
			}
			q, ok := DefaultModelPolicies()[id]
			return q, ok
		}); err != nil {
			return err
		}
	}
	if old, ok := before[model]; ok && old == p {
		return nil
	}
	oldRev := s.st.ModelRoutingRevision
	rev, err := bumpRevision(oldRev)
	if err != nil {
		return err
	}
	s.st.ModelRoutingRevision = rev
	s.st.ModelPolicies = next
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err == nil {
		err = s.writeState(raw)
	}
	if err != nil {
		s.st.ModelPolicies = before
		s.st.ModelRoutingRevision = oldRev
		if strict {
			return config.ModelFailure(500, "model_save_failed", "模型策略保存失败")
		}
	}
	return err
}
