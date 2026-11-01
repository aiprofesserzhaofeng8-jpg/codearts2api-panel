package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"codearts2api/internal/accounts"
	"codearts2api/internal/apierr"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// A plan contains only catalog models allowed by the authenticated group.
// Selection/leases stay in Store; platform adapters receive only concrete models.
type groupModelPlan struct {
	public string
	models map[string]registry.Model
	ids    []string
}

func mappedPlan(ctx context.Context) *groupModelPlan {
	if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
		return c.mapping
	}
	return nil
}
func explicitModel(name string) bool { return strings.Contains(name, "/") }
func (s *Server) mappingPlan(access store.AccessScope, name string) *groupModelPlan {
	sources, found := access.Mapping(name)
	if !found {
		return nil
	}
	plan := &groupModelPlan{public: name, models: map[string]registry.Model{}}
	accounts := s.Store.Accounts()
	for _, id := range sources {
		if m, ok := s.Reg.ResolveRaw(id); ok && scopeAllows(access, accounts, m) {
			plan.models[m.ID] = m
			plan.ids = append(plan.ids, m.ID)
		}
	}
	return plan
}
func publicMappingModel(name string) registry.Model {
	return registry.Model{ID: name, Name: name, Provider: "group", Source: "mapping"}
}
func (s *Server) resolveMappedModel(r *http.Request, settings store.Settings, access store.AccessScope, requested string) (registry.Model, error) {
	if settings.ForceModel != "" {
		return registry.Model{}, &provider.Error{Status: 400, Message: "分组统一模型与强制模型不能同时使用"}
	}
	name := strings.TrimSpace(requested)
	if name == "" {
		name = settings.DefaultModel
	}
	if explicitModel(name) {
		m, ok := s.Reg.ResolveRaw(name)
		if !ok {
			return registry.Model{}, &apierr.ModelNotFound{Name: name}
		}
		if !scopeAllows(access, s.Store.Accounts(), m) {
			return registry.Model{}, groupDenied()
		}
		return m, nil
	}
	plan := s.mappingPlan(access, name)
	if plan == nil {
		return registry.Model{}, &apierr.ModelNotFound{Name: name}
	}
	if len(plan.ids) == 0 {
		return registry.Model{}, fmt.Errorf("%w：统一模型没有可用来源", apierr.NoAccount)
	}
	if c, ok := r.Context().Value(usageContextKey{}).(*usageCapture); ok {
		c.mapping = plan
	}
	return publicMappingModel(name), nil
}

func (s *Server) prepareMappedAttempt(ctx context.Context, acct *accounts.Account, model registry.Model, body []byte) (registry.Model, []byte, error) {
	plan := mappedPlan(ctx)
	if plan == nil {
		return model, body, nil
	}
	id, _ := ctx.Value(routingModelKey{}).(string)
	actual, ok := plan.models[id]
	if !ok || modelProvider(actual) != acct.ProviderID() {
		return model, nil, fmt.Errorf("统一模型未选定合法上游")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return model, nil, err
	}
	obj["model"], _ = json.Marshal(actual.ID)
	raw, err := json.Marshal(obj)
	return actual, raw, err
}

// Known Messages limitation: exclude Trae before selection if thinking control is requested.
func filterMappedMessages(ctx context.Context, thinking bool) error {
	plan := mappedPlan(ctx)
	if plan == nil || !thinking {
		return nil
	}
	ids := make([]string, 0, len(plan.ids))
	for _, id := range plan.ids {
		if modelProvider(plan.models[id]) != provider.Trae {
			ids = append(ids, id)
		}
	}
	plan.ids = ids
	if len(ids) == 0 {
		return &provider.Error{Status: 400, Message: "此统一模型没有支持 Messages thinking 控制的来源"}
	}
	return nil
}

// A logical model advertises only recommendations safe for every eligible source.
// Never resolve its public name through legacy aliases or infer native capabilities.
func (s *Server) requestModelPolicy(r *http.Request, model registry.Model) (store.ModelPolicy, bool) {
	if model.Source != "mapping" {
		return s.modelPolicyFor(model)
	}
	access, err := s.Store.AccessScope(groupKey(r.Context()))
	if err != nil {
		return store.ModelPolicy{}, false
	}
	plan := s.mappingPlan(access, model.ID)
	if plan == nil || len(plan.ids) == 0 {
		return store.ModelPolicy{}, false
	}
	var combined store.ModelPolicy
	for i, id := range plan.ids {
		p, ok := s.modelPolicyFor(plan.models[id])
		if !ok {
			return store.ModelPolicy{}, false
		}
		if i == 0 {
			combined = p
		} else {
			combined.ContextWindow = min(combined.ContextWindow, p.ContextWindow)
			combined.MaxOutputTokens = min(combined.MaxOutputTokens, p.MaxOutputTokens)
			combined.TriggerTokens = min(combined.TriggerTokens, p.TriggerTokens)
			combined.KeepRecentTokens = min(combined.KeepRecentTokens, p.KeepRecentTokens)
			combined.ReserveTokens = max(combined.ReserveTokens, p.ReserveTokens)
		}
	}
	combined.ReserveTokens = min(combined.ReserveTokens, combined.ContextWindow-combined.TriggerTokens)
	combined.SummaryEnabled, combined.SummaryRetry = false, false
	combined.SummaryFallbackModel = ""
	combined.SummaryMaxTokens = combined.MaxOutputTokens
	combined.Source = "分组内各来源的保守公共建议；实际能力以所选上游为准"
	return combined, combined.Validate() == nil
}

// Existing same-platform summary fallback remains subject to the raw allowlist.
func (s *Server) mappedSummaryFallback(ctx context.Context, target string) bool {
	if requestKind(ctx) != "summary" || summaryAttempts(ctx) == 0 {
		return false
	}
	base, _ := ctx.Value(routingModelKey{}).(string)
	p, ok := s.Store.ModelPolicy(base)
	return ok && p.SummaryRetry && p.SummaryFallbackModel == target
}

// Rewrite just the protocol envelope. Raw fields retain exact integers/tool JSON.
// Only mapped Chat requests need this; the two translated protocols already use
// the logical name. Ordinary forwarding has no additional response processing.
func mappedResponseModel(raw []byte, name string) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("上游响应不是 JSON 对象")
	}
	obj["model"], _ = json.Marshal(name)
	return json.Marshal(obj)
}
