package config

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"codearts2api/internal/provider"
)

// Platform settings own search and optional field-level common prompt overrides.
type ProviderSettings struct {
	PromptOverrides *PromptOverrides `json:"prompt_overrides"`
	PromptMode      string           `json:"prompt_mode"`
	PromptText      string           `json:"prompt_text"`
	PromptFallback  bool             `json:"prompt_fallback"`
	Sanitize        bool             `json:"sanitize"`
	SearchEnabled   bool             `json:"search_enabled"`
	SearchScope     string           `json:"search_scope"`
	SearchBackend   string           `json:"search_backend"`
	SearchURL       string           `json:"search_url"`
}

func DefaultProviderSettings(name string) ProviderSettings {
	w := ProviderSettings{PromptMode: "passthrough", PromptFallback: true, Sanitize: true, SearchScope: "cn", SearchBackend: "searxng"}
	if name == provider.WorkBuddy {
		w.PromptMode, w.SearchEnabled, w.SearchBackend = "custom", true, "native"
	}
	if name == provider.Qoder || name == provider.Kiro || name == provider.ChatGPT || name == provider.Claude {
		w.PromptOverrides = &PromptOverrides{}
	}
	return w
}

func (w ProviderSettings) ValidateFor(name string) error {
	if name != provider.WorkBuddy && name != provider.CodeArts && name != provider.Trae && name != provider.Qoder && name != provider.Kiro && name != provider.ChatGPT && name != provider.Claude {
		return fmt.Errorf("未知平台配置")
	}
	if w.PromptMode != "custom" && w.PromptMode != "passthrough" {
		return fmt.Errorf("%s 提示词模式须为 custom 或 passthrough", name)
	}
	if len(w.PromptText) > 64<<10 || !utf8.ValidString(w.PromptText) {
		return fmt.Errorf("%s 提示词须为有效 UTF-8 且不超过 64 KiB", name)
	}
	if w.SearchScope != "cn" && !(name == provider.WorkBuddy && w.SearchScope == "global") {
		return fmt.Errorf("%s 搜索地区无效", name)
	}
	if (name == provider.Qoder || name == provider.Kiro || name == provider.ChatGPT || name == provider.Claude) && (w.SearchEnabled || w.SearchURL != "") {
		return fmt.Errorf("%s 尚未接入独立联网搜索", name)
	}
	if name == provider.WorkBuddy {
		if w.SearchBackend != "" && w.SearchBackend != "native" || w.SearchURL != "" {
			return fmt.Errorf("WorkBuddy 使用原生搜索，不接受外部搜索地址")
		}
		return nil
	}
	if w.SearchBackend != "searxng" {
		return fmt.Errorf("%s 当前支持独立 SearXNG 搜索后端", name)
	}
	if w.SearchURL == "" && !w.SearchEnabled {
		return nil
	}
	u, err := url.Parse(w.SearchURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(w.SearchURL, "\r\n") || len(w.SearchURL) > 2048 {
		return fmt.Errorf("%s 需要有效的 SearXNG HTTP(S) 搜索地址，不可包含凭据、查询参数或片段", name)
	}
	return nil
}

func (s Settings) ProviderPolicy(name string) ProviderSettings {
	return s.ConfiguredProvider(name).ResolvePrompt(s.CommonPrompt)
}
