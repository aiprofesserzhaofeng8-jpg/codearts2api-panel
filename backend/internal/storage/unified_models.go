package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"codearts2api/internal/registry"
)

func (s *Store) UnifiedSnapshot() config.UnifiedSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := make(map[string]ModelPolicy, len(s.st.ModelPolicies))
	for k, v := range s.st.ModelPolicies {
		p[k] = v
	}
	return config.UnifiedSnapshot{Revision: s.st.ModelRoutingRevision, Models: config.CloneUnifiedModels(s.st.UnifiedModels), Policies: p}
}
func checkRevision(expected, actual uint64) error {
	if expected == 0 {
		return config.ModelFailure(428, "revision_required", "缺少配置版本条件")
	}
	if expected != actual {
		return config.ModelFailure(409, "revision_conflict", "配置已被修改，请刷新后重试")
	}
	return nil
}
func bumpRevision(v uint64) (uint64, error) {
	if !config.ValidModelRevision(v) || v == config.MaxModelRevision {
		return v, config.ModelFailure(409, "revision_conflict", "配置版本已达上限")
	}
	return v + 1, nil
}
func reservedName(st *State, name string) bool {
	for _, n := range registry.ReservedNames() {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	for n := range st.Settings.ModelAliases {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}
func modelIndex(st *State, id string) int {
	for i, m := range st.UnifiedModels {
		if m.ID == id {
			return i
		}
	}
	return -1
}
func sourceSet(m config.UnifiedModel) map[string]bool {
	out := map[string]bool{}
	for _, src := range m.Sources {
		out[src.ID] = true
	}
	return out
}

// SaveUnifiedModel atomically commits the definition, its optional policy and
// routing revision. SourcePolicies contains non-secret catalog recommendations
// prepared by catalog; persisted raw policies always take precedence.
func (s *Store) SaveUnifiedModel(m config.UnifiedModel, p *config.ModelPolicy, expected, route uint64, sourcePolicies map[string]config.ModelPolicy) (config.UnifiedModel, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := checkRevision(route, s.st.ModelRoutingRevision); err != nil {
		return m, err
	}
	idx := modelIndex(s.st, m.ID)
	if m.ID == "" {
		if expected != 0 {
			return m, config.ModelFailure(409, "revision_conflict", "新模型不应指定旧版本")
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return m, err
		}
		m.ID = "um_" + hex.EncodeToString(id[:])
		m.Revision = 1
	} else {
		if idx < 0 {
			return m, config.ModelFailure(404, "model_not_found", "统一模型不存在")
		}
		old := s.st.UnifiedModels[idx]
		if err := checkRevision(expected, old.Revision); err != nil {
			return m, err
		}
		if old.Name != m.Name {
			return m, config.ModelFailure(400, "immutable_model_name", "公开名称不可直接修改")
		}
		m.Revision = old.Revision
	}
	if err := m.Validate(); err != nil {
		return m, err
	}
	if idx < 0 && len(s.st.UnifiedModels) >= config.MaxUnifiedModels {
		return m, config.ModelFailure(400, "invalid_model_definition", "统一模型数量已达上限")
	}
	if s.st.Settings.ForceModel != "" || reservedName(s.st, m.Name) {
		return m, config.ModelFailure(409, "model_name_conflict", "统一名称与旧名称或强制模型冲突")
	}
	for _, old := range s.st.UnifiedModels {
		if (idx < 0 && old.ID == m.ID) || (old.ID != m.ID && strings.EqualFold(old.Name, m.Name)) {
			return m, config.ModelFailure(409, "model_name_conflict", "统一名称重复")
		}
	}
	allowed := sourceSet(m)
	for _, g := range s.st.Groups {
		for _, grant := range g.UnifiedModels {
			if grant.ModelID == m.ID && grant.SourceMode == "selected" {
				for _, id := range grant.SourceIDs {
					if !allowed[id] {
						return m, config.ModelFailure(409, "model_in_use", "分组仍引用要删除的来源")
					}
				}
			}
		}
	}
	if p != nil {
		err := config.ValidateUnifiedPolicy(*p, m.Sources, func(id string) (config.ModelPolicy, bool) {
			if q, ok := s.st.ModelPolicies[id]; ok {
				return q, true
			}
			if q, ok := DefaultModelPolicies()[id]; ok {
				return q, true
			}
			q, ok := sourcePolicies[id]
			return q, ok
		})
		if err != nil {
			return m, err
		}
	}
	key := config.UnifiedPolicyKey(m.ID)
	oldPolicy, has := s.st.ModelPolicies[key]
	if idx >= 0 && reflect.DeepEqual(m, s.st.UnifiedModels[idx]) && ((p == nil && !has) || (p != nil && has && oldPolicy == *p)) {
		return config.CloneUnifiedModel(m), nil
	}
	if idx >= 0 {
		var err error
		m.Revision, err = bumpRevision(m.Revision)
		if err != nil {
			return m, err
		}
	}
	nextRev, err := bumpRevision(s.st.ModelRoutingRevision)
	if err != nil {
		return m, err
	}
	before, policies, revision := s.st.UnifiedModels, s.st.ModelPolicies, s.st.ModelRoutingRevision
	s.st.UnifiedModels = config.CloneUnifiedModels(before)
	if idx < 0 {
		s.st.UnifiedModels = append(s.st.UnifiedModels, config.CloneUnifiedModel(m))
	} else {
		s.st.UnifiedModels[idx] = config.CloneUnifiedModel(m)
	}
	s.st.ModelPolicies = clonePolicyMap(policies)
	if p == nil {
		delete(s.st.ModelPolicies, key)
	} else {
		s.st.ModelPolicies[key] = *p
	}
	s.st.ModelRoutingRevision = nextRev
	if err = s.saveGroupStateLocked(); err != nil {
		s.st.UnifiedModels, s.st.ModelPolicies, s.st.ModelRoutingRevision = before, policies, revision
		return m, config.ModelFailure(500, "model_save_failed", "统一模型保存失败")
	}
	return config.CloneUnifiedModel(m), nil
}
func clonePolicyMap(in map[string]ModelPolicy) map[string]ModelPolicy {
	out := make(map[string]ModelPolicy, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (s *Store) DeleteUnifiedModel(id string, expected, route uint64) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := checkRevision(route, s.st.ModelRoutingRevision); err != nil {
		return err
	}
	idx := modelIndex(s.st, id)
	if idx < 0 {
		return config.ModelFailure(404, "model_not_found", "统一模型不存在")
	}
	m := s.st.UnifiedModels[idx]
	if err := checkRevision(expected, m.Revision); err != nil {
		return err
	}
	if s.st.Settings.DefaultModel == m.Name {
		return config.ModelFailure(409, "model_in_use", "全局默认模型仍引用此模型")
	}
	for _, g := range s.st.Groups {
		if g.DefaultModel == m.Name || g.ClientDefaultModel == m.Name {
			return config.ModelFailure(409, "model_in_use", "分组默认值仍引用此模型")
		}
		for _, a := range g.UnifiedModels {
			if a.ModelID == id {
				return config.ModelFailure(409, "model_in_use", "分组仍引用此模型")
			}
		}
	}
	rev, err := bumpRevision(s.st.ModelRoutingRevision)
	if err != nil {
		return err
	}
	before, policies, oldRev := s.st.UnifiedModels, s.st.ModelPolicies, s.st.ModelRoutingRevision
	s.st.UnifiedModels = append(config.CloneUnifiedModels(before[:idx]), config.CloneUnifiedModels(before[idx+1:])...)
	s.st.ModelPolicies = clonePolicyMap(policies)
	delete(s.st.ModelPolicies, config.UnifiedPolicyKey(id))
	s.st.ModelRoutingRevision = rev
	if err = s.saveGroupStateLocked(); err != nil {
		s.st.UnifiedModels, s.st.ModelPolicies, s.st.ModelRoutingRevision = before, policies, oldRev
		return config.ModelFailure(500, "model_save_failed", "统一模型删除失败")
	}
	return nil
}

// projectGroupLocked is a temporary view for pre-U3 consumers, never a second
// persisted mapping. Raw IDs retain their existing policy and accounting keys.
func (s *Store) projectGroupLocked(g AccountGroup) AccountGroup {
	g = cloneGroup(g)
	g.ModelMappings = nil
	for _, a := range g.UnifiedModels {
		idx := modelIndex(s.st, a.ModelID)
		if idx < 0 {
			continue
		}
		m := s.st.UnifiedModels[idx]
		ids := []string{}
		for _, src := range m.Sources {
			if !m.Enabled || !src.Enabled {
				continue
			}
			allowed := a.SourceMode == "all"
			for _, id := range a.SourceIDs {
				allowed = allowed || id == src.ID
			}
			if allowed {
				ids = append(ids, config.RawSource(src.ID))
			}
		}
		g.ModelMappings = append(g.ModelMappings, ModelMapping{Name: m.Name, Sources: ids})
		visible, callable := a.Visible, a.Callable
		if !visible || !callable {
			g.ModelRules = append(g.ModelRules, access.ModelRule{ID: m.Name, Visible: &visible, Callable: &callable})
		}
	}
	return g
}

func validateUnifiedGroup(g AccountGroup, models []config.UnifiedModel) error {
	if !config.ValidModelRevision(g.Revision) || (g.RawModelMode != "legacy" && g.RawModelMode != "qualified_only") || g.UnifiedModels == nil {
		return config.ModelFailure(400, "invalid_model_definition", "分组统一模型字段无效")
	}
	if len(g.UnifiedModels) > 128 {
		return config.ModelFailure(400, "invalid_model_definition", "分组统一模型过多")
	}
	byID := map[string]config.UnifiedModel{}
	for _, m := range models {
		byID[m.ID] = m
	}
	seen := map[string]bool{}
	for _, a := range g.UnifiedModels {
		m, ok := byID[a.ModelID]
		if !ok || seen[a.ModelID] || a.SourceIDs == nil || (a.SourceMode != "all" && a.SourceMode != "selected") || len(a.SourceIDs) > 16 || (a.SourceMode == "all" && len(a.SourceIDs) != 0) {
			return config.ModelFailure(400, "invalid_model_definition", "分组授权无效")
		}
		seen[a.ModelID] = true
		allowed := sourceSet(m)
		ids := map[string]bool{}
		for _, id := range a.SourceIDs {
			if !allowed[id] || ids[id] {
				return config.ModelFailure(400, "invalid_model_definition", "分组来源不存在或重复")
			}
			ids[id] = true
		}
	}
	for _, name := range []string{g.DefaultModel, g.ClientDefaultModel} {
		if name == "" {
			continue
		}
		found := false
		for _, m := range models {
			if m.Name != name {
				continue
			}
			found = true
			ok := false
			for _, a := range g.UnifiedModels {
				if a.ModelID != m.ID || !a.Callable || (name == g.ClientDefaultModel && !a.Visible) {
					continue
				}
				for _, src := range m.Sources {
					scope := access.AccessScope{Restricted: true, AllowlistEnabled: g.AllowlistEnabled, ModelIDs: map[string]bool{}, ModelRules: g.ModelRules}
					for _, id := range g.ModelIDs {
						scope.ModelIDs[id] = true
					}
					if !scope.AllowsModel(config.RawSource(src.ID)) {
						continue
					}
					if a.SourceMode == "all" {
						ok = true
					}
					for _, id := range a.SourceIDs {
						ok = ok || id == src.ID
					}
				}
			}
			if !ok {
				return config.ModelFailure(409, "model_in_use", "默认模型没有允许的来源")
			}
		}
		if !found && g.RawModelMode == "qualified_only" && !strings.Contains(name, "/") {
			return config.ModelFailure(409, "model_in_use", "默认模型不在统一授权中")
		}
		if !found {
			raw := config.RawSource(name)
			scope := access.AccessScope{Restricted: true, AllowlistEnabled: g.AllowlistEnabled, ModelRules: g.ModelRules, ModelIDs: map[string]bool{}}
			for _, id := range g.ModelIDs {
				scope.ModelIDs[id] = true
			}
			if !scope.AllowsModel(raw) || (name == g.ClientDefaultModel && !scope.Visible(raw, g.RawModelsVisibleDefault)) {
				return config.ModelFailure(409, "model_in_use", "原始默认模型不在授权范围")
			}
		}
	}
	return nil
}

func validateUnifiedState(st *State) error {
	if !config.ValidModelRevision(st.ModelRoutingRevision) || st.UnifiedModels == nil || len(st.UnifiedModels) > config.MaxUnifiedModels {
		return config.ModelFailure(400, "invalid_model_definition", "统一目录或版本缺失")
	}
	names, ids := map[string]bool{}, map[string]bool{}
	for _, m := range st.UnifiedModels {
		if err := m.Validate(); err != nil {
			return err
		}
		n := strings.ToLower(m.Name)
		if names[n] || ids[m.ID] || reservedName(st, m.Name) {
			return config.ModelFailure(409, "model_name_conflict", "统一模型名称冲突")
		}
		names[n] = true
		ids[m.ID] = true
	}
	if len(st.UnifiedModels) > 0 && st.Settings.ForceModel != "" {
		return config.ModelFailure(409, "model_name_conflict", "统一模型与强制型号冲突")
	}
	groupIDs := map[string]bool{}
	for _, g := range st.Groups {
		if g.ID == "" || groupIDs[g.ID] {
			return config.ModelFailure(400, "invalid_model_definition", "分组标识缺失或重复")
		}
		groupIDs[g.ID] = true
		if len(g.ModelMappings) > 0 {
			return config.ModelFailure(400, "legacy_mapping_field", "新状态不能保存旧分组映射")
		}
		if err := validateUnifiedGroup(g, st.UnifiedModels); err != nil {
			return err
		}
	}
	for key, p := range st.ModelPolicies {
		if !strings.HasPrefix(key, "unified/") {
			continue
		}
		id := strings.TrimPrefix(key, "unified/")
		if !ids[id] || p.Validate() != nil || p.SummaryEnabled || p.SummaryRetry || p.SummaryFallbackModel != "" || p.DisableSummaryThinking {
			return config.ModelFailure(400, "invalid_model_definition", "统一策略引用或字段无效")
		}
	}
	return nil
}

func sortedSources(ids []string) []config.ModelSource {
	out := make([]config.ModelSource, len(ids))
	for i, id := range ids {
		out[i] = config.ModelSource{ID: config.CanonicalSource(id), Enabled: true}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// decodeNewRouting checks presence before decoding, so missing false and [] do
// not become permissive defaults. Historical state remains a separate input.
func decodeNewRouting(raw []byte) error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	for _, k := range []string{"unified_models", "model_routing_revision"} {
		if v, ok := doc[k]; !ok || string(v) == "null" {
			return config.ModelFailure(400, "invalid_model_definition", "状态缺少统一模型字段")
		}
	}
	var groups []map[string]json.RawMessage
	if err := json.Unmarshal(doc["groups"], &groups); doc["groups"] != nil && err != nil {
		return err
	}
	for _, g := range groups {
		if _, ok := g["model_mappings"]; ok {
			return config.ModelFailure(400, "legacy_mapping_field", "新状态包含旧映射字段")
		}
		for _, k := range []string{"revision", "unified_models", "raw_model_mode", "raw_models_visible_default"} {
			v, ok := g[k]
			if !ok || string(v) == "null" {
				return config.ModelFailure(400, "invalid_model_definition", "新状态缺少分组权限字段")
			}
		}
	}
	return nil
}
