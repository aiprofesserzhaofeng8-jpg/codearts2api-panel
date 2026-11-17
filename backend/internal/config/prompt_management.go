package config

import "fmt"

var promptProviders = []string{"codearts", "workbuddy", "trae", "qoder", "kiro", "chatgpt", "claude", "teleagent"}

// PromptConfiguration projects only prompt-owned fields. Search, credentials,
// routing and pool configuration never travel through this management endpoint.
type PromptConfiguration struct {
	Common    PromptSettings            `json:"common"`
	Platforms map[string]PlatformPrompt `json:"platforms"`
}
type PlatformPrompt struct {
	PromptSettings
	Overrides *PromptOverrides `json:"overrides"`
}

func (s Settings) PromptConfiguration() PromptConfiguration {
	out := PromptConfiguration{Common: s.CommonPrompt, Platforms: map[string]PlatformPrompt{}}
	for _, name := range promptProviders {
		p := s.ConfiguredProvider(name).Clone()
		out.Platforms[name] = PlatformPrompt{PromptSettings: PromptSettings{Mode: p.PromptMode, Text: p.PromptText, Fallback: p.PromptFallback, Sanitize: p.Sanitize}, Overrides: p.PromptOverrides}
	}
	return out
}
func (p PromptConfiguration) Validate() error {
	if err := p.Common.Validate(); err != nil {
		return err
	}
	if len(p.Platforms) != len(promptProviders) {
		return fmt.Errorf("平台提示词配置不完整，请重新读取")
	}
	for _, name := range promptProviders {
		entry, ok := p.Platforms[name]
		if !ok {
			return fmt.Errorf("缺少 %s 提示词配置", name)
		}
		if err := entry.PromptSettings.Validate(); err != nil {
			return err
		}
		value := DefaultProviderSettings(name)
		value.PromptMode, value.PromptText, value.PromptFallback, value.Sanitize, value.PromptOverrides = entry.Mode, entry.Text, entry.Fallback, entry.Sanitize, entry.Overrides
		effective := value.ResolvePrompt(p.Common)
		if err := (PromptSettings{Mode: effective.PromptMode, Text: effective.PromptText, Fallback: effective.PromptFallback, Sanitize: effective.Sanitize}).Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (p PromptConfiguration) Apply(s *Settings) {
	s.CommonPrompt = p.Common
	destinations := map[string]*ProviderSettings{"codearts": &s.CodeArts, "workbuddy": &s.WorkBuddy, "trae": &s.Trae, "qoder": &s.Qoder, "kiro": &s.Kiro, "chatgpt": &s.ChatGPT, "claude": &s.Claude, "teleagent": &s.TeleAgent}
	for name, dest := range destinations {
		entry := p.Platforms[name]
		dest.PromptMode, dest.PromptText, dest.PromptFallback, dest.Sanitize, dest.PromptOverrides = entry.Mode, entry.Text, entry.Fallback, entry.Sanitize, entry.Overrides
		*dest = dest.Clone()
	}
}
