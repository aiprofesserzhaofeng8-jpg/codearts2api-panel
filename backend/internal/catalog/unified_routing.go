package catalog

import (
	"encoding/json"
	"strings"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// Inspect content fields, not schemas or argument values; never rewrite input.
func (p *ModelPlan) FilterCapabilities(body []byte) error {
	if p == nil || p.Selection.ID == "" {
		return nil
	}
	var request struct {
		Tools    []json.RawMessage `json:"tools"`
		Messages []struct {
			Role      string            `json:"role"`
			Content   json.RawMessage   `json:"content"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
		MaxTokens           json.Number `json:"max_tokens"`
		MaxCompletionTokens json.Number `json:"max_completion_tokens"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return config.ModelFailure(400, "model_capability_unavailable", "无法解析模型能力请求")
	}
	image, tool := false, len(request.Tools) > 0
	for _, message := range request.Messages {
		tool = tool || message.Role == "tool" || len(message.ToolCalls) > 0
		var parts []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(message.Content, &parts) == nil {
			for _, part := range parts {
				image = image || part.Type == "image_url" || part.Type == "input_image" || part.Type == "image"
			}
		}
	}
	limit, _ := request.MaxTokens.Int64()
	if x, err := request.MaxCompletionTokens.Int64(); err == nil && x > limit {
		limit = x
	}
	ids := make([]string, 0, len(p.IDs))
	for _, id := range p.IDs {
		m := p.Models[id]
		if image && m.ImageInput != "supported" {
			continue
		}
		if tool && m.ToolCalling != "supported" {
			continue
		}
		if limit > 0 && m.MaxOutput > 0 && limit > int64(m.MaxOutput) {
			continue
		}
		ids = append(ids, id)
	}
	p.IDs = ids
	if len(ids) == 0 {
		return config.ModelFailure(400, "model_capability_unavailable", "此统一模型没有明确支持所需图片、工具或输出限制的来源")
	}
	return nil
}

type UnifiedRoutingStore interface{ UnifiedSnapshot() config.UnifiedSnapshot }

func unifiedDenied() error {
	return config.ModelFailure(403, "model_permission_denied", "当前分组不允许调用此统一模型或来源")
}
func unifiedUnavailable() error {
	return config.ModelFailure(503, "model_sources_unavailable", "此统一模型暂无可用来源，请检查来源目录和账号状态")
}

func (s *Routing) unifiedPlan(snapshot config.UnifiedSnapshot, scope access.AccessScope, m config.UnifiedModel) (*ModelPlan, error) {
	return s.buildUnifiedPlan(snapshot, scope, m, false)
}

func (s *Routing) buildUnifiedPlan(snapshot config.UnifiedSnapshot, scope access.AccessScope, m config.UnifiedModel, profile bool) (*ModelPlan, error) {
	grant, ok := scope.UnifiedGrant(m.ID)
	if !ok || !grant.Callable {
		return nil, unifiedDenied()
	}
	plan := &ModelPlan{Public: m.Name, Selection: access.UnifiedSelection{ID: m.ID, Revision: m.Revision, RoutingRevision: snapshot.Revision}, Models: map[string]provider.Model{}, IDs: []string{}}
	authorized := 0
	for _, src := range m.Sources {
		if !grant.AllowsSource(src.ID) || !scope.AllowsModel(config.RawSource(src.ID)) {
			continue
		}
		authorized++
		if !m.Enabled || !src.Enabled {
			continue
		}
		raw, ok := s.directory.ResolveRaw(src.ID)
		if !ok {
			if profile {
				id := config.RawSource(src.ID)
				plan.IDs = append(plan.IDs, id)
				platform, region, _ := provider.ParseModel(id)
				plan.Models[id] = provider.Model{ID: id, Provider: platform, Scope: region}
			}
			continue
		}
		plan.IDs = append(plan.IDs, raw.ID)
		plan.Models[raw.ID] = raw
	}
	if authorized == 0 {
		return nil, unifiedDenied()
	}
	if !m.Enabled || (!profile && len(plan.IDs) == 0) {
		return nil, unifiedUnavailable()
	}
	return plan, nil
}

func (s *Routing) resolveUnified(snapshot config.UnifiedSnapshot, scope access.AccessScope, settings config.Settings, requested string) (provider.Model, *ModelPlan, error) {
	return s.resolveUnifiedMode(snapshot, scope, settings, requested, false)
}

func (s *Routing) resolveUnifiedMode(snapshot config.UnifiedSnapshot, scope access.AccessScope, settings config.Settings, requested string, profile bool) (provider.Model, *ModelPlan, error) {
	name := strings.TrimSpace(requested)
	if settings.ForceModel != "" {
		if len(snapshot.Models) > 0 {
			return provider.Model{}, nil, config.ModelFailure(409, "model_name_conflict", "统一模型与强制型号不能同时使用")
		}
		name = settings.ForceModel
	}
	if name == "" {
		name = scope.DefaultModel
		if name == "" {
			name = settings.DefaultModel
		}
	}
	if !strings.Contains(name, "/") {
		for _, m := range snapshot.Models {
			if m.Name == name {
				plan, err := s.buildUnifiedPlan(snapshot, scope, m, profile)
				if err != nil {
					return provider.Model{}, nil, err
				}
				return plan.PublicModel(), plan, nil
			}
		}
		for _, m := range snapshot.Models {
			if strings.EqualFold(m.Name, name) {
				return provider.Model{}, nil, &provider.ModelNotFound{Name: name}
			}
		}
		if scope.Restricted && scope.RawModelMode == "qualified_only" {
			return provider.Model{}, nil, &provider.ModelNotFound{Name: name}
		}
	}
	var raw provider.Model
	var ok bool
	if strings.Contains(name, "/") {
		raw, ok = s.directory.ResolveRaw(name)
	} else {
		raw, ok = s.directory.Resolve(name, settings.ModelAliases)
	}
	if !ok {
		return provider.Model{}, nil, &provider.ModelNotFound{Name: name}
	}
	if !scope.Callable(name) || !ScopeAllows(scope, s.store.Accounts(), raw) {
		return provider.Model{}, nil, GroupDenied()
	}
	return raw, nil, nil
}

func (s *Routing) checkUnified(scope access.AccessScope, rawID string, plan *ModelPlan) error {
	st, ok := s.store.(UnifiedRoutingStore)
	if !ok {
		return unifiedDenied()
	}
	snapshot := st.UnifiedSnapshot()
	for _, m := range snapshot.Models {
		if m.ID != plan.Selection.ID {
			continue
		}
		grant, ok := scope.UnifiedGrant(m.ID)
		if !ok || !grant.Callable || !grant.AllowsSource(config.CanonicalSource(rawID)) || !scope.AllowsModel(rawID) {
			return unifiedDenied()
		}
		if snapshot.Revision != plan.Selection.RoutingRevision || m.Revision != plan.Selection.Revision {
			return config.ModelFailure(409, "model_configuration_changed", "模型配置已变化，请重新提交请求")
		}
		if !m.Enabled {
			return unifiedUnavailable()
		}
		for _, src := range m.Sources {
			if config.RawSource(src.ID) == rawID && src.Enabled {
				return nil
			}
		}
		return unifiedUnavailable()
	}
	return unifiedDenied()
}
