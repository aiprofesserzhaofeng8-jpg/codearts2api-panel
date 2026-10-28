package server

import (
	"strings"

	"codearts2api/internal/apierr"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// resolveModel 把请求里的模型名解析为上游模型。
//
// 优先级：强制模型 → 请求里的 model → 控制台默认模型。
// 解析失败返回 *apierr.ModelNotFound（对外 404）—— 现在每个模型都真实扣积分，
// 绝不静默回落到别的模型。
func (s *Server) resolveModel(settings store.Settings, requested string) (registry.Model, error) {
	name := strings.TrimSpace(requested)
	if settings.ForceModel != "" {
		name = settings.ForceModel
	}
	if name == "" {
		name = settings.DefaultModel
	}
	m, ok := s.Reg.Resolve(name, settings.ModelAliases)
	if !ok {
		return registry.Model{}, &apierr.ModelNotFound{
			Name:      name,
			Available: s.availableModelIDs(settings),
		}
	}
	if strings.Contains(requested, "/") {
		p, scope, _ := provider.ParseModel(requested)
		if p != modelProvider(m) || scope != m.Scope {
			return registry.Model{}, &provider.Error{Status: 400, Message: "模型别名或强制模型指向其他上游/地区，请检查模型设置"}
		}
	}
	return m, nil
}

// availableModelIDs 返回当前可用模型 ID，用于错误提示。
func (s *Server) availableModelIDs(settings store.Settings) []string {
	list := s.Reg.List(settings.ModelAliases)
	ids := make([]string, 0, len(list))
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	return ids
}
