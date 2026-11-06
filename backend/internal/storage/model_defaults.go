package storage

import (
	"reflect"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// ApplyDefaultModels is one durable transaction. Failure never leaves a partial
// directory or grants. Existing definitions retain IDs, names, disabled sources
// and policies; group grants are additive only for previously ungranted models.
func (s *Store) ApplyDefaultModels(models []config.UnifiedModel, expected uint64, groupID string, groupRevision uint64, policies map[string]config.ModelPolicy) (snapshot config.UnifiedSnapshot, err error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = checkRevision(expected, s.st.ModelRoutingRevision); err != nil {
		return snapshot, err
	}
	if len(models) == 0 || len(models) > 512 {
		return snapshot, config.ModelFailure(400, "invalid_model_definition", "默认模型数量无效")
	}
	gi := -1
	if groupID != "" {
		for i, g := range s.st.Groups {
			if g.ID == groupID {
				gi = i
				break
			}
		}
		if gi < 0 {
			return snapshot, config.ModelFailure(404, "model_not_found", "分组不存在")
		}
		if err = checkRevision(groupRevision, s.st.Groups[gi].Revision); err != nil {
			return snapshot, err
		}
	} else if groupRevision != 0 {
		return snapshot, config.ModelFailure(400, "invalid_model_definition", "未选择分组不能指定分组版本")
	}
	beforeModels, beforePolicies, beforeGroups, beforeRevision := s.st.UnifiedModels, s.st.ModelPolicies, s.st.Groups, s.st.ModelRoutingRevision
	defer func() {
		if err != nil {
			s.st.UnifiedModels, s.st.ModelPolicies, s.st.Groups, s.st.ModelRoutingRevision = beforeModels, beforePolicies, beforeGroups, beforeRevision
		}
	}()
	saved := []config.UnifiedModel{}
	seen := map[string]bool{}
	for _, model := range models {
		if seen[model.Name] {
			return snapshot, config.ModelFailure(400, "invalid_model_definition", "默认模型重复")
		}
		seen[model.Name] = true
		var policy *config.ModelPolicy
		if p, ok := s.st.ModelPolicies[config.UnifiedPolicyKey(model.ID)]; ok {
			policy = &p
		}
		value, e := s.saveUnifiedModelLocked(model, policy, model.Revision, s.st.ModelRoutingRevision, policies, false)
		if e != nil {
			return snapshot, e
		}
		saved = append(saved, value)
	}
	if gi >= 0 {
		group := cloneGroup(s.st.Groups[gi])
		before := cloneGroup(group)
		scope := access.AccessScope{Restricted: true, AllowlistEnabled: group.AllowlistEnabled, ModelIDs: map[string]bool{}, ModelRules: group.ModelRules}
		for _, id := range group.ModelIDs {
			scope.ModelIDs[id] = true
		}
		granted := map[string]bool{}
		for _, g := range group.UnifiedModels {
			granted[g.ModelID] = true
		}
		accounts := map[string]bool{}
		for _, id := range group.AccountIDs {
			accounts[id] = true
		}
		for _, model := range saved {
			if granted[model.ID] || !model.Enabled {
				continue
			}
			ids := []string{}
			for _, src := range model.Sources {
				raw := config.RawSource(src.ID)
				if !src.Enabled || !scope.AllowsModel(raw) {
					continue
				}
				p, region, _ := provider.ParseModel(raw)
				for _, account := range s.st.Accounts {
					if accounts[account.ID] && account.Enabled && account.ProviderID() == p && (p == provider.CodeArts || account.Scope == region) {
						ids = append(ids, src.ID)
						break
					}
				}
			}
			if len(ids) > 0 {
				group.UnifiedModels = append(group.UnifiedModels, access.UnifiedGrant{ModelID: model.ID, Visible: true, Callable: true, SourceMode: "selected", SourceIDs: ids})
			}
		}
		if !reflect.DeepEqual(before, group) {
			group.Revision, err = bumpRevision(group.Revision)
			if err != nil {
				return snapshot, err
			}
			if err = validateUnifiedGroup(group, s.st.UnifiedModels); err != nil {
				return snapshot, err
			}
			s.st.Groups = append([]AccountGroup(nil), s.st.Groups...)
			s.st.Groups[gi] = group
			if s.st.ModelRoutingRevision == beforeRevision {
				s.st.ModelRoutingRevision, err = bumpRevision(beforeRevision)
				if err != nil {
					return snapshot, err
				}
			}
		}
	}
	if s.st.ModelRoutingRevision != beforeRevision {
		if err = s.saveGroupStateLocked(); err != nil {
			return snapshot, config.ModelFailure(500, "model_save_failed", "默认模型保存失败，未应用任何修改")
		}
	}
	return config.UnifiedSnapshot{Revision: s.st.ModelRoutingRevision, Models: config.CloneUnifiedModels(s.st.UnifiedModels), Policies: clonePolicyMap(s.st.ModelPolicies)}, nil
}
