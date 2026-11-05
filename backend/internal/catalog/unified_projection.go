package catalog

import (
	"strings"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// Listing is an authorization projection, never an availability/lease check.
func (s *Routing) unifiedModelsForScope(snapshot config.UnifiedSnapshot, scope access.AccessScope) []provider.Model {
	out := []provider.Model{}
	covered, names, seen := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, m := range snapshot.Models {
		names[strings.ToLower(m.Name)] = true
		grant, ok := scope.UnifiedGrant(m.ID)
		if !m.Enabled || !ok || !grant.Visible {
			continue
		}
		plan := &ModelPlan{Public: m.Name, Selection: access.UnifiedSelection{ID: m.ID}, Models: map[string]provider.Model{}}
		for _, src := range m.Sources {
			if !grant.AllowsSource(src.ID) {
				continue
			}
			covered[config.RawSource(src.ID)] = true
			if src.Enabled && scope.AllowsModel(config.RawSource(src.ID)) {
				raw, found := s.directory.ResolveRaw(src.ID)
				if !found {
					raw = provider.Model{ID: config.RawSource(src.ID)}
				}
				plan.IDs = append(plan.IDs, raw.ID)
				plan.Models[raw.ID] = raw
			}
		}
		out = append(out, plan.PublicModel())
		seen[m.Name] = true
	}
	listingScope := scope
	listingScope.ModelRules = nil // callable must not determine visibility
	settings, accounts := s.store.Settings(), s.store.Accounts()
	for _, row := range s.directory.List(settings.ModelAliases) {
		raw, ok := s.directory.Resolve(row.ID, settings.ModelAliases)
		if !ok || !ScopeAllows(listingScope, accounts, raw) {
			continue
		}
		fallback := !scope.Restricted || scope.RawModelsVisibleDefault
		fallback = fallback && !covered[raw.ID]
		if !scope.Visible(config.CanonicalSource(raw.ID), scope.Visible(raw.ID, fallback)) {
			continue
		}
		if covered[raw.ID] || (scope.Restricted && scope.RawModelMode == "qualified_only") || names[strings.ToLower(row.ID)] {
			row.ID = config.CanonicalSource(raw.ID)
			row.Source = raw.Source
		}
		if !seen[row.ID] {
			out = append(out, row)
			seen[row.ID] = true
		}
	}
	return out
}

// Unified recommendations use every enabled authorized configured source,
// including temporarily missing catalog entries with an existing raw policy.
// Accounts, health and callable/visible flags never manufacture token limits.
func (s *Routing) unifiedPolicy(snapshot config.UnifiedSnapshot, scope access.AccessScope, name string, policies *config.ModelPolicies) (config.ModelPolicy, bool) {
	for _, m := range snapshot.Models {
		if m.Name == name {
			return s.policyForUnified(snapshot, scope, m)
		}
	}
	return config.ModelPolicy{}, false
}

func (s *Routing) policyForUnified(snapshot config.UnifiedSnapshot, scope access.AccessScope, m config.UnifiedModel) (config.ModelPolicy, bool) {
	if !m.Enabled {
		return config.ModelPolicy{}, false
	}
	grant, ok := scope.UnifiedGrant(m.ID)
	if !ok || (!grant.Callable && !grant.Visible) {
		return config.ModelPolicy{}, false
	}
	ids := []string{}
	lookup := func(id string) (config.ModelPolicy, bool) {
		raw, ok := s.directory.ResolveRaw(id)
		if !ok {
			p, region, _ := provider.ParseModel(id)
			raw = provider.Model{ID: config.RawSource(id), Provider: p, Scope: region}
		}
		if p, ok := snapshot.Policies[raw.ID]; ok {
			return p, true
		}
		if p, ok := config.DefaultModelPolicies()[raw.ID]; ok {
			return p, true
		}
		return config.RecommendModelPolicy(raw)
	}
	for _, src := range m.Sources {
		if src.Enabled && grant.AllowsSource(src.ID) && scope.AllowsModel(config.RawSource(src.ID)) {
			ids = append(ids, src.ID)
		}
	}
	if len(ids) == 0 {
		return config.ModelPolicy{}, false
	}
	if p, ok := snapshot.Policies[config.UnifiedPolicyKey(m.ID)]; ok {
		sources := make([]config.ModelSource, len(ids))
		for i, id := range ids {
			sources[i] = config.ModelSource{ID: id, Enabled: true}
		}
		return p, config.ValidateUnifiedPolicy(p, sources, lookup) == nil
	}
	p, ok := config.CommonModelPolicy(ids, lookup)
	p.Source = "统一模型各授权来源的保守公共建议；实际能力以所选上游为准"
	return p, ok
}

type ClientModel struct {
	Model     provider.Model
	Policy    *config.ModelPolicy
	Selection access.UnifiedSelection
}

// ClientModels uses one configuration/authorization snapshot for the entire
// listing. No per-row full-directory copies or account leases are created.
func (s *Routing) ClientModels(key string, policies *config.ModelPolicies) ([]ClientModel, error) {
	scope, err := s.store.AccessScope(key)
	if err != nil {
		return nil, GroupDenied()
	}
	var snapshot config.UnifiedSnapshot
	var models []provider.Model
	if st, ok := s.store.(UnifiedRoutingStore); ok {
		snapshot = st.UnifiedSnapshot()
		models = s.unifiedModelsForScope(snapshot, scope)
	} else {
		models = s.ModelsForScope(scope)
	}
	byName := map[string]config.UnifiedModel{}
	for _, model := range snapshot.Models {
		byName[model.Name] = model
	}
	out := make([]ClientModel, 0, len(models))
	settings := s.store.Settings()
	for _, model := range models {
		row := ClientModel{Model: model}
		var p config.ModelPolicy
		var ok bool
		if m, found := byName[model.ID]; found && model.Provider == "unified" {
			p, ok = s.policyForUnified(snapshot, scope, m)
			row.Selection = access.UnifiedSelection{ID: m.ID, Revision: m.Revision, RoutingRevision: snapshot.Revision}
		} else if raw, found := s.directory.Resolve(model.ID, settings.ModelAliases); found {
			p, ok = policies.For(raw)
		} else if model.Source == "mapping" {
			p, ok = s.ModelPolicy(key, model, policies)
		}
		if ok {
			row.Policy = &p
		}
		out = append(out, row)
	}
	return out, nil
}

// Profile shares request resolution but does not require a catalog/available
// account when a configured unified recommendation is still fully grounded.
func (s *Routing) Profile(key, requested string, policies *config.ModelPolicies) (provider.Model, *ModelPlan, config.ModelPolicy, error) {
	scope, err := s.store.AccessScope(key)
	if err != nil {
		return provider.Model{}, nil, config.ModelPolicy{}, GroupDenied()
	}
	if strings.TrimSpace(requested) == "" {
		requested = scope.ClientDefaultModel
	}
	var model provider.Model
	var plan *ModelPlan
	if st, ok := s.store.(UnifiedRoutingStore); ok {
		snapshot := st.UnifiedSnapshot()
		model, plan, err = s.resolveUnifiedMode(snapshot, scope, s.store.Settings(), requested, true)
		if err == nil && plan != nil {
			if p, ok := s.unifiedPolicy(snapshot, scope, model.ID, policies); ok {
				return model, plan, p, nil
			}
			return model, plan, config.ModelPolicy{}, config.ModelFailure(422, "client_profile_missing", "此统一模型缺少来源客户端参数，请在模型页补齐")
		}
	} else {
		model, plan, err = s.Resolve(key, s.store.Settings(), requested)
	}
	if err != nil {
		return model, plan, config.ModelPolicy{}, err
	}
	if p, ok := s.ModelPolicy(key, model, policies); ok {
		return model, plan, p, nil
	}
	return model, plan, config.ModelPolicy{}, config.ModelFailure(422, "client_profile_missing", "该模型暂无客户端推荐参数，请在模型页配置并验证")
}
