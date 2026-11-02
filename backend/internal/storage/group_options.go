package storage

import (
	"codearts2api/internal/access"
	"fmt"
	"strings"
)

// Rules add no new permission domain. They can narrow calls, or change listing;
// membership, the physical allowlist and current mapping still apply separately.
func validateGroupOptions(g AccountGroup) error {
	if len(g.ModelRules) > 2048 {
		return fmt.Errorf("模型规则过多")
	}
	scope := access.AccessScope{ModelRules: g.ModelRules}
	seen := map[string]bool{}
	for _, r := range g.ModelRules {
		if r.ID == "" || len(r.ID) > 256 || strings.TrimSpace(r.ID) != r.ID || seen[r.ID] {
			return fmt.Errorf("模型规则标识无效或重复：%s", r.ID)
		}
		seen[r.ID] = true
	}
	for _, name := range []string{g.DefaultModel, g.ClientDefaultModel} {
		if name == "" {
			continue
		}
		if strings.TrimSpace(name) != name || len(name) > 256 || !scope.Callable(name) {
			return fmt.Errorf("默认模型须为允许调用的模型")
		}
		if len(g.ModelMappings) > 0 && !strings.Contains(name, "/") {
			found := false
			for _, m := range g.ModelMappings {
				if m.Name == name {
					found = true
					usable := false
					for _, id := range m.Sources {
						if scope.Callable(id) {
							usable = true
						}
					}
					if !usable {
						return fmt.Errorf("默认模型没有允许调用的来源：%s", name)
					}
				}
			}
			if !found {
				return fmt.Errorf("默认模型不在分组统一模型中：%s", name)
			}
		} else if g.AllowlistEnabled {
			found := false
			for _, id := range g.ModelIDs {
				if id == name {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("默认模型不在分组白名单中：%s", name)
			}
		}
		if name == g.ClientDefaultModel && !scope.Visible(name, len(g.ModelMappings) == 0 || !strings.Contains(name, "/")) {
			return fmt.Errorf("客户端默认模型必须可见")
		}
	}
	return nil
}
