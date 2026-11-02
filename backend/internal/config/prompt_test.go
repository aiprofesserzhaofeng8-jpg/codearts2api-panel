package config

import (
	"reflect"
	"testing"
)

func TestPromptInheritanceLegacyFalseEmptyAndIsolation(t *testing.T) {
	mode := "custom"
	empty := ""
	off := false
	common := PromptSettings{Mode: mode, Text: "COMMON", Fallback: true, Sanitize: true}
	legacy := DefaultProviderSettings("workbuddy")
	legacy.PromptText = "LEGACY"
	if got := legacy.ResolvePrompt(common); !reflect.DeepEqual(got, legacy) {
		t.Fatal("migration changed legacy", got)
	}
	inherited := DefaultProviderSettings("trae")
	inherited.PromptOverrides = &PromptOverrides{Text: &empty, Fallback: &off, Sanitize: &off}
	got := inherited.ResolvePrompt(common)
	if got.PromptMode != mode || got.PromptText != "" || got.PromptFallback || got.Sanitize || got.SearchEnabled != inherited.SearchEnabled || got.SearchBackend != inherited.SearchBackend {
		t.Fatal(got)
	}
	copy := inherited.Clone()
	*copy.PromptOverrides.Text = "MUTATED"
	*copy.PromptOverrides.Fallback = true
	if *inherited.PromptOverrides.Text != "" || *inherited.PromptOverrides.Fallback {
		t.Fatal("snapshot shares mutable override")
	}
	st := Settings{CommonPrompt: common, WorkBuddy: legacy, Trae: inherited, CodeArts: DefaultProviderSettings("codearts")}
	if st.ProviderPolicy("workbuddy").PromptText != "LEGACY" || st.ProviderPolicy("codearts").PromptMode != "passthrough" || st.ProviderPolicy("trae").PromptMode != "custom" {
		t.Fatal("platform cross contamination")
	}
}
