package catalog

import (
	"fmt"
	"strings"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// Virtual capabilities are namespaced separately from the conversation catalog.
// CodeArts has no region segment in its legacy model IDs.
func SearchModel(name, scope string) provider.Model {
	id := provider.ModelID(name, scope, "web-search")
	if name == provider.CodeArts {
		id = "codearts/web-search"
		scope = ""
	}
	return provider.Model{ID: id, Provider: name, Scope: scope, Name: name + " 联网搜索"}
}

func ResolveSearchTarget(settings config.Settings, requested string) (provider.Model, config.ProviderSettings, error) {
	name, scope := provider.WorkBuddy, settings.WorkBuddy.SearchScope
	if strings.Contains(requested, "/") {
		var id string
		name, scope, id = provider.ParseModel(requested)
		switch name {
		case provider.CodeArts:
			if id != "web-search" {
				return provider.Model{}, config.ProviderSettings{}, fmt.Errorf("CodeArts 搜索标识须为 codearts/web-search")
			}
		case provider.Trae:
			if scope != "cn" || id != "web-search" {
				return provider.Model{}, config.ProviderSettings{}, fmt.Errorf("Trae 搜索标识须为 trae/cn/web-search")
			}
		case provider.WorkBuddy:
			if (scope != "cn" && scope != "global") || id == "" {
				return provider.Model{}, config.ProviderSettings{}, fmt.Errorf("WorkBuddy 搜索地区无效")
			}
		default:
			return provider.Model{}, config.ProviderSettings{}, fmt.Errorf("未知搜索平台，不会回落到其他平台")
		}
	}
	return SearchModel(name, scope), settings.ProviderPolicy(name), nil
}
