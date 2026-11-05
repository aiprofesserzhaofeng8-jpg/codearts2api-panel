package storage

import (
	"reflect"
	"strings"

	"codearts2api/internal/config"
)

func copyAliases(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func routingSettingsChanged(a, b Settings) bool {
	return a.DefaultModel != b.DefaultModel || a.ForceModel != b.ForceModel || !reflect.DeepEqual(a.ModelAliases, b.ModelAliases)
}
func validateRoutingSettings(st *State, next Settings) error {
	if len(st.UnifiedModels) > 0 && next.ForceModel != "" {
		return config.ModelFailure(409, "model_name_conflict", "统一模型与强制型号冲突")
	}
	for _, m := range st.UnifiedModels {
		for alias, target := range next.ModelAliases {
			if strings.EqualFold(alias, m.Name) || target == m.Name {
				return config.ModelFailure(409, "model_name_conflict", "旧别名不能覆盖或引用统一名称")
			}
		}
	}
	return nil
}
func (s *Store) UpdateSettingsChecked(expected uint64, fn func(*Settings)) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := checkRevision(expected, s.st.ModelRoutingRevision); err != nil {
		return err
	}
	next := s.st.Settings
	next.ModelAliases = copyAliases(next.ModelAliases)
	fn(&next)
	if err := validateRoutingSettings(s.st, next); err != nil {
		return err
	}
	before, rev := s.st.Settings, s.st.ModelRoutingRevision
	if routingSettingsChanged(before, next) {
		n, err := bumpRevision(rev)
		if err != nil {
			return err
		}
		s.st.ModelRoutingRevision = n
	}
	s.st.Settings = next
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.Settings, s.st.ModelRoutingRevision = before, rev
		return config.ModelFailure(500, "model_save_failed", "模型设置保存失败")
	}
	return nil
}

func (s *Store) SetModelPolicyChecked(model string, p ModelPolicy, expected uint64) error {
	return s.setModelPolicy(model, p, expected, true)
}
