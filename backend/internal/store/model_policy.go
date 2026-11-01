package store

import (
	"codearts2api/internal/config"
	"encoding/json"
	"fmt"
	"strings"
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
