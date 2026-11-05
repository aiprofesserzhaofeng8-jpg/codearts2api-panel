package storage

import (
	"codearts2api/internal/access"
	"codearts2api/internal/config"
)

// Caller holds Store.mu; the current definition and grant are checked in the
// same transaction that chooses the actual account and occupies its lease.
func (s *Store) unifiedSourcesLocked(scope access.AccessScope, public string, candidates []string, selection access.UnifiedSelection) ([]string, error) {
	denied := func() error {
		return config.ModelFailure(403, "model_permission_denied", "当前分组不允许调用此统一模型或来源")
	}
	i := modelIndex(s.st, selection.ID)
	if i < 0 {
		return nil, denied()
	}
	m := s.st.UnifiedModels[i]
	grant, ok := scope.UnifiedGrant(m.ID)
	if !ok || !grant.Callable || public != m.Name {
		return nil, denied()
	}
	allowed := []string{}
	authorized := 0
	for _, src := range m.Sources {
		raw := config.RawSource(src.ID)
		if !grant.AllowsSource(src.ID) || !scope.AllowsModel(raw) {
			continue
		}
		authorized++
		if !m.Enabled || !src.Enabled {
			continue
		}
		for _, id := range candidates {
			if id == raw {
				allowed = append(allowed, raw)
				break
			}
		}
	}
	if authorized == 0 {
		return nil, denied()
	}
	if m.Revision != selection.Revision || s.st.ModelRoutingRevision != selection.RoutingRevision {
		return nil, config.ModelFailure(409, "model_configuration_changed", "模型配置已变化，请重新提交请求")
	}
	if !m.Enabled {
		return nil, config.ModelFailure(503, "model_sources_unavailable", "此统一模型已停用")
	}
	if len(allowed) == 0 {
		return nil, denied()
	}
	return allowed, nil
}
