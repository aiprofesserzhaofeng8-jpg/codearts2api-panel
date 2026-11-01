package catalog

import (
	"errors"
	"fmt"
	"strings"

	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// RoutingStore supplies current read-only permission/account projections.
type RoutingStore interface {
	Accounts() []*accounts.Account
	AccessScope(string) (access.AccessScope, error)
	Settings() config.Settings
}
type RoutingDirectory interface {
	Resolve(string, map[string]string) (provider.Model, bool)
	ResolveRaw(string) (provider.Model, bool)
	List(map[string]string) []provider.Model
}

// Routing borrows the existing directory and current permission snapshots. It
// never admits requests, selects accounts, stores plans, or caches permissions.
type Routing struct {
	store     RoutingStore
	directory RoutingDirectory
}

func NewRouting(st RoutingStore, directory RoutingDirectory) *Routing {
	return &Routing{store: st, directory: directory}
}

// ModelPlan belongs to one request. Only its candidate IDs may be narrowed for
// protocol capability checks; atomic selection and reauthorization stay in Store.
type ModelPlan struct {
	Public string
	Models map[string]provider.Model
	IDs    []string
}

func ModelProvider(m provider.Model) string {
	if m.Provider == "" {
		return provider.CodeArts
	}
	return m.Provider
}
func GroupDenied() error {
	return &provider.Error{Status: 403, Message: "当前 API Key 的分组不允许调用此模型或没有对应账号"}
}
func ScopeAllows(scope access.AccessScope, list []*accounts.Account, model provider.Model) bool {
	if !scope.Restricted {
		return true
	}
	if !scope.AllowsModel(model.ID) {
		return false
	}
	for _, a := range list {
		if scope.AccountIDs[a.ID] && a.Enabled && a.ProviderID() == ModelProvider(model) && (a.ProviderID() == provider.CodeArts || a.Scope == model.Scope) {
			return true
		}
	}
	return false
}
func PublicMappingModel(name string) provider.Model {
	return provider.Model{ID: name, Name: name, Provider: "group", Source: "mapping"}
}
func (s *Routing) MappingPlan(scope access.AccessScope, name string) *ModelPlan {
	sources, found := scope.Mapping(name)
	if !found {
		return nil
	}
	plan := &ModelPlan{Public: name, Models: map[string]provider.Model{}}
	list := s.store.Accounts()
	for _, id := range sources {
		if m, ok := s.directory.ResolveRaw(id); ok && ScopeAllows(scope, list, m) {
			plan.Models[m.ID] = m
			plan.IDs = append(plan.IDs, m.ID)
		}
	}
	return plan
}
func (s *Routing) AvailableModelIDs(settings config.Settings) []string {
	list := s.directory.List(settings.ModelAliases)
	ids := make([]string, 0, len(list))
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	return ids
}
func (s *Routing) ResolveDefault(settings config.Settings, requested string) (provider.Model, error) {
	name := strings.TrimSpace(requested)
	if settings.ForceModel != "" {
		name = settings.ForceModel
	}
	if name == "" {
		name = settings.DefaultModel
	}
	m, ok := s.directory.Resolve(name, settings.ModelAliases)
	if !ok {
		return provider.Model{}, &provider.ModelNotFound{Name: name, Available: s.AvailableModelIDs(settings)}
	}
	if strings.Contains(requested, "/") {
		p, scope, _ := provider.ParseModel(requested)
		if p != ModelProvider(m) || scope != m.Scope {
			return provider.Model{}, &provider.Error{Status: 400, Message: "模型别名或强制模型指向其他上游/地区，请检查模型设置"}
		}
	}
	return m, nil
}
func (s *Routing) ResolveMapped(settings config.Settings, scope access.AccessScope, requested string) (provider.Model, *ModelPlan, error) {
	if settings.ForceModel != "" {
		return provider.Model{}, nil, &provider.Error{Status: 400, Message: "分组统一模型与强制模型不能同时使用"}
	}
	name := strings.TrimSpace(requested)
	if name == "" {
		name = settings.DefaultModel
	}
	if strings.Contains(name, "/") {
		m, ok := s.directory.ResolveRaw(name)
		if !ok {
			return provider.Model{}, nil, &provider.ModelNotFound{Name: name}
		}
		if !ScopeAllows(scope, s.store.Accounts(), m) {
			return provider.Model{}, nil, GroupDenied()
		}
		return m, nil, nil
	}
	plan := s.MappingPlan(scope, name)
	if plan == nil {
		return provider.Model{}, nil, &provider.ModelNotFound{Name: name}
	}
	if len(plan.IDs) == 0 {
		return provider.Model{}, nil, fmt.Errorf("%w：统一模型没有可用来源", accounts.ErrNoAccount)
	}
	return PublicMappingModel(name), plan, nil
}
func (s *Routing) VisibleModels(key string) ([]provider.Model, error) {
	scope, err := s.store.AccessScope(key)
	if err != nil {
		return nil, GroupDenied()
	}
	settings := s.store.Settings()
	list := s.store.Accounts()
	out := []provider.Model{}
	if len(scope.ModelMappings) > 0 {
		for _, mapping := range scope.ModelMappings {
			plan := s.MappingPlan(scope, mapping.Name)
			if len(plan.IDs) > 0 {
				out = append(out, PublicMappingModel(mapping.Name))
			}
		}
		return out, nil
	}
	for _, m := range s.directory.List(settings.ModelAliases) {
		resolved, ok := s.directory.Resolve(m.ID, settings.ModelAliases)
		if ok && ScopeAllows(scope, list, resolved) {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *Routing) Resolve(key string, settings config.Settings, requested string) (provider.Model, *ModelPlan, error) {
	scope, err := s.store.AccessScope(key)
	if err != nil {
		return provider.Model{}, nil, GroupDenied()
	}
	if len(scope.ModelMappings) > 0 {
		return s.ResolveMapped(settings, scope, requested)
	}
	m, err := s.ResolveDefault(settings, requested)
	if err != nil {
		var missing *provider.ModelNotFound
		if scope.Restricted && errors.As(err, &missing) {
			list, _ := s.VisibleModels(key)
			ids := []string{}
			for _, m := range list {
				ids = append(ids, m.ID)
			}
			return provider.Model{}, nil, &provider.ModelNotFound{Name: requested, Available: ids}
		}
		return provider.Model{}, nil, err
	}
	if !ScopeAllows(scope, s.store.Accounts(), m) {
		return provider.Model{}, nil, GroupDenied()
	}
	return m, nil, nil
}

// Check rereads permissions at the original budget/admission boundary. Fallback
// permission is evaluated lazily, only for a live mapping lacking this source.
func (s *Routing) Check(key, model string, search bool, plan *ModelPlan, fallback func() bool) error {
	scope, err := s.store.AccessScope(key)
	if err != nil {
		return GroupDenied()
	}
	if !scope.Restricted {
		return nil
	}
	if search {
		target, _, err := ResolveSearchTarget(s.store.Settings(), model)
		if err != nil || target.ID != model || !ScopeAllows(scope, s.store.Accounts(), target) {
			return GroupDenied()
		}
		return nil
	}
	if plan != nil {
		sources, found := scope.Mapping(plan.Public)
		allowed := false
		for _, id := range sources {
			if id == model {
				allowed = true
			}
		}
		if !found || (!allowed && !fallback()) {
			return GroupDenied()
		}
	}
	m, ok := s.directory.Resolve(model, s.store.Settings().ModelAliases)
	if !ok || !ScopeAllows(scope, s.store.Accounts(), m) {
		return GroupDenied()
	}
	return nil
}
func (p *ModelPlan) Selected(id, platform string) (provider.Model, error) {
	actual, ok := p.Models[id]
	if !ok || ModelProvider(actual) != platform {
		return provider.Model{}, fmt.Errorf("统一模型未选定合法上游")
	}
	return actual, nil
}
func (p *ModelPlan) FilterMessages(thinking bool) error {
	if p == nil || !thinking {
		return nil
	}
	ids := make([]string, 0, len(p.IDs))
	for _, id := range p.IDs {
		if ModelProvider(p.Models[id]) != provider.Trae {
			ids = append(ids, id)
		}
	}
	p.IDs = ids
	if len(ids) == 0 {
		return &provider.Error{Status: 400, Message: "此统一模型没有支持 Messages thinking 控制的来源"}
	}
	return nil
}
func (s *Routing) ModelPolicy(key string, model provider.Model, policies *config.ModelPolicies) (config.ModelPolicy, bool) {
	if model.Source != "mapping" {
		return policies.For(model)
	}
	scope, err := s.store.AccessScope(key)
	if err != nil {
		return config.ModelPolicy{}, false
	}
	plan := s.MappingPlan(scope, model.ID)
	if plan == nil || len(plan.IDs) == 0 {
		return config.ModelPolicy{}, false
	}
	return config.CommonModelPolicy(plan.IDs, func(id string) (config.ModelPolicy, bool) { return policies.For(plan.Models[id]) })
}
