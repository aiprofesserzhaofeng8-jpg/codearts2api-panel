package storage

import (
	"fmt"
	"strings"
	"unicode"

	"codearts2api/internal/access"
	"codearts2api/internal/provider"
)

// ModelMapping is local to one group. Sources are concrete catalog IDs, not aliases.
type ModelMapping = access.ModelMapping

func cloneMappings(in []ModelMapping) []ModelMapping {
	if in == nil {
		return nil
	}
	out := make([]ModelMapping, len(in))
	for i, m := range in {
		out[i] = m
		out[i].Sources = append([]string(nil), m.Sources...)
	}
	return out
}
func validateMappings(mappings []ModelMapping) error {
	if len(mappings) > 128 {
		return fmt.Errorf("每组最多配置 128 个统一模型")
	}
	seen := map[string]bool{}
	for _, m := range mappings {
		digit := strings.ContainsAny(m.Name, "0123456789")
		if m.Name == "" || len(m.Name) > 200 || strings.Contains(m.Name, "/") || strings.IndexFunc(m.Name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || !digit {
			return fmt.Errorf("统一模型名称须包含版本号且不含空白或斜线，例如 deepseek-4.1-flash")
		}
		if seen[strings.ToLower(m.Name)] {
			return fmt.Errorf("统一模型名称重复：%s", m.Name)
		}
		seen[strings.ToLower(m.Name)] = true
		if len(m.Sources) == 0 || len(m.Sources) > 16 {
			return fmt.Errorf("%s 须选择 1–16 个原始型号", m.Name)
		}
		platforms := map[string]bool{}
		for _, id := range m.Sources {
			p, scope, _ := provider.ParseModel(id)
			if id == "" || len(id) > 256 || (p != provider.CodeArts && p != provider.WorkBuddy && p != provider.Trae) {
				return fmt.Errorf("%s 的原始型号无效", m.Name)
			}
			k := p + "/" + scope
			if platforms[k] {
				return fmt.Errorf("%s 在同一平台和地区只能选择一个原始型号", m.Name)
			}
			platforms[k] = true
		}
	}
	return nil
}
