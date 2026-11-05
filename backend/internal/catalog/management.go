package catalog

import (
	"strings"

	"codearts2api/internal/config"
)

type UnifiedModelStore interface {
	UnifiedSnapshot() config.UnifiedSnapshot
	Settings() config.Settings
	SaveUnifiedModel(config.UnifiedModel, *config.ModelPolicy, uint64, uint64, map[string]config.ModelPolicy) (config.UnifiedModel, error)
	DeleteUnifiedModel(string, uint64, uint64) error
}
type Management struct {
	store     UnifiedModelStore
	directory RoutingDirectory
	policies  *config.ModelPolicies
}

func NewManagement(st UnifiedModelStore, dir RoutingDirectory, p *config.ModelPolicies) *Management {
	return &Management{st, dir, p}
}
func (m *Management) Snapshot() config.UnifiedSnapshot { return m.store.UnifiedSnapshot() }
func (m *Management) Delete(id string, revision, routing uint64) error {
	return m.store.DeleteUnifiedModel(id, revision, routing)
}
func (m *Management) Save(model config.UnifiedModel, p *config.ModelPolicy, revision, routing uint64) (config.UnifiedModel, error) {
	snap := m.store.UnifiedSnapshot()
	old := map[string]bool{}
	for _, x := range snap.Models {
		if x.ID == model.ID {
			for _, s := range x.Sources {
				old[s.ID] = true
			}
		}
	}
	model = config.CloneUnifiedModel(model)
	for _, raw := range m.directory.List(m.store.Settings().ModelAliases) {
		if model.ID != "" {
			break
		} // existing public meaning survives new catalog collisions
		if !strings.Contains(raw.ID, "/") && strings.EqualFold(raw.ID, model.Name) {
			return model, config.ModelFailure(409, "model_name_conflict", "公开名称与原始型号或别名冲突")
		}
	}
	policies := map[string]config.ModelPolicy{}
	for i, src := range model.Sources {
		raw, ok := m.directory.ResolveRaw(src.ID)
		if !ok {
			if old[src.ID] {
				continue
			}
			return model, config.ModelFailure(400, "invalid_model_definition", "新来源不在当前目录")
		}
		model.Sources[i].ID = config.CanonicalSource(raw.ID)
		if m.policies != nil {
			if policy, ok := m.policies.For(raw); ok {
				policies[raw.ID] = policy
			}
		}
	}
	return m.store.SaveUnifiedModel(model, p, revision, routing, policies)
}
