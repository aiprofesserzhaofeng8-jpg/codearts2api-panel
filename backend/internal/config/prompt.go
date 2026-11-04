package config

import (
	"fmt"
	"unicode/utf8"
)

// PromptSettings is a value snapshot shared only as defaults. Search settings
// remain platform-owned; neither prompts nor search ever inherit another platform.
type PromptSettings struct {
	Mode     string `json:"prompt_mode"`
	Text     string `json:"prompt_text"`
	Fallback bool   `json:"prompt_fallback"`
	Sanitize bool   `json:"sanitize"`
}

// Nil field inherits; pointers preserve explicit false and explicit empty text.
// A nil Overrides object retains the pre-M6 platform configuration verbatim.
type PromptOverrides struct {
	Mode     *string `json:"prompt_mode,omitempty"`
	Text     *string `json:"prompt_text,omitempty"`
	Fallback *bool   `json:"prompt_fallback,omitempty"`
	Sanitize *bool   `json:"sanitize,omitempty"`
}

func DefaultPromptSettings() PromptSettings {
	return PromptSettings{Mode: "passthrough", Fallback: true, Sanitize: true}
}
func (p PromptSettings) Validate() error {
	if p.Mode != "custom" && p.Mode != "passthrough" {
		return fmt.Errorf("提示词模式须为 custom 或 passthrough")
	}
	if len(p.Text) > 64<<10 || !utf8.ValidString(p.Text) {
		return fmt.Errorf("提示词须为有效 UTF-8 且不超过 64 KiB")
	}
	// Fallback is dormant in custom mode, so changing mode never adds a retry.
	return nil
}
func (w ProviderSettings) ResolvePrompt(common PromptSettings) ProviderSettings {
	o := w.PromptOverrides
	if o == nil {
		return w
	}
	w.PromptMode, w.PromptText, w.PromptFallback, w.Sanitize = common.Mode, common.Text, common.Fallback, common.Sanitize
	if o.Mode != nil {
		w.PromptMode = *o.Mode
	}
	if o.Text != nil {
		w.PromptText = *o.Text
	}
	if o.Fallback != nil {
		w.PromptFallback = *o.Fallback
	}
	if o.Sanitize != nil {
		w.Sanitize = *o.Sanitize
	}
	return w
}
func (w ProviderSettings) Clone() ProviderSettings {
	if w.PromptOverrides == nil {
		return w
	}
	// A single allocation owns the full immutable snapshot of optional fields.
	snapshot := new(struct {
		Overrides          PromptOverrides
		Mode, Text         string
		Fallback, Sanitize bool
	})
	o := w.PromptOverrides
	if o.Mode != nil {
		snapshot.Mode = *o.Mode
		snapshot.Overrides.Mode = &snapshot.Mode
	}
	if o.Text != nil {
		snapshot.Text = *o.Text
		snapshot.Overrides.Text = &snapshot.Text
	}
	if o.Fallback != nil {
		snapshot.Fallback = *o.Fallback
		snapshot.Overrides.Fallback = &snapshot.Fallback
	}
	if o.Sanitize != nil {
		snapshot.Sanitize = *o.Sanitize
		snapshot.Overrides.Sanitize = &snapshot.Sanitize
	}
	w.PromptOverrides = &snapshot.Overrides
	return w
}

// ConfiguredProvider is for editors. Request execution uses ProviderPolicy.
func (s Settings) ConfiguredProvider(name string) ProviderSettings {
	switch name {
	case "codearts":
		return s.CodeArts
	case "trae":
		return s.Trae
	case "qoder":
		return s.Qoder
	case "kiro":
		return s.Kiro
	case "claude":
		return s.Claude
	case "chatgpt":
		return s.ChatGPT
	case "workbuddy":
		return s.WorkBuddy
	}
	return ProviderSettings{}
}
