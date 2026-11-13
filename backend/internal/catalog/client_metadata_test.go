package catalog

import (
	"testing"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func TestClientMetadataKeepsObservedBoundsAndUnknownSourcesIndependent(t *testing.T) {
	model := provider.Model{ID: "qoder/cn/gfmodel", Name: "GLM-5.3-Flash", Provider: provider.Qoder, Scope: "cn", Source: "upstream"}
	p, ok := RecommendClientPolicy(model)
	if !ok || p.ContextWindow != 32768 || p.MaxOutputTokens != 4096 || p.SummaryEnabled || model.Context != 0 || model.MaxOutput != 0 {
		t.Fatal("reviewed recommendation fabricated provider limits", p, model)
	}
	_, _, evidence := sourceClientPolicy(model, nil)
	if evidence.Basis != "tested_recommendation" || evidence.Reference == "" || evidence.VerifiedAt != "2026-11-07" || evidence.ProviderContext != 0 {
		t.Fatal("lost evidence scope", evidence)
	}
	if evidence.OfficialContext != "1M" || evidence.OfficialMaxOutput != "128K" || evidence.ContextWindow != 32768 {
		t.Fatal("official labels replaced a source recommendation", evidence)
	}
	model.Context, model.MaxOutput = 16384, 2048
	p, ok = RecommendClientPolicy(model)
	if !ok || p.ContextWindow != 16384 || p.MaxOutputTokens != 2048 {
		t.Fatal("reviewed defaults raised smaller provider bounds", p)
	}
	model.Context, model.MaxOutput, model.Name = 0, 0, "Changed identity"
	_, ok, evidence = sourceClientPolicy(model, nil)
	if !ok || evidence.Basis != "compatibility_default" || evidence.VerifiedAt != "" {
		t.Fatal("changed raw slot inherited reviewed evidence", evidence)
	}
	if _, ok := RecommendClientPolicy(provider.Model{ID: "glm-5.3-flash"}); ok {
		t.Fatal("missing directory inherited verified metadata")
	}
}

func TestClientCompatibilitySeparatesPurposeLimitsAndAdministratorConfig(t *testing.T) {
	chat := provider.Model{ID: "workbuddy/cn/model", Name: "Model", Provider: provider.WorkBuddy, Context: 65536, MaxOutput: 8192}
	p, _ := RecommendClientPolicy(chat)
	override := p
	override.ContextWindow = 32768
	override.TriggerTokens = 16000
	override.KeepRecentTokens = 6000
	configured := map[string]config.ModelPolicy{chat.ID: override}
	got := clientCompatibility([]provider.Model{chat}, &override, configured)
	if !got.Exportable || got.Purpose != "chat" || got.Limits[0].Basis != "administrator_config" || got.Limits[0].ProviderContext != 65536 || got.Limits[0].ContextWindow != 32768 {
		t.Fatal(got)
	}
	for id, purpose := range map[string]string{"workbuddy/cn/codewise-completions": "completion", "workbuddy/cn/codewise-jump": "jump", "workbuddy/cn/codewise-rewrite": "rewrite", "workbuddy/cn/hunyuan-image-alpha": "image_generation", "trae/cn/summary": "summary", "trae/cn/file_search_agent": "agent"} {
		model := chat
		model.ID = id
		got = clientCompatibility([]provider.Model{model}, &p, nil)
		if got.Exportable || got.Purpose != purpose || got.Status != "unsupported_purpose" || got.Reason == "" {
			t.Fatal(id, got)
		}
	}
	auto := chat
	auto.ID = "kiro/eu-central-1/auto"
	got = clientCompatibility([]provider.Model{chat, auto}, &p, nil)
	if !got.Exportable || got.Purpose != "auto" || got.Reason == "" {
		t.Fatal("automatic route lost scope", got)
	}
	got = clientCompatibility([]provider.Model{chat, {ID: "trae/cn/missing"}}, nil, nil)
	if got.Exportable || got.Reason == "" || len(got.Limits) != 2 || got.Limits[1].Basis != "unknown" {
		t.Fatal("missing source silently raised recommendation", got)
	}
}

func TestSharedClientPolicyRequiresEverySourceAndKeepsConservativeMinimum(t *testing.T) {
	models := []provider.Model{{ID: "workbuddy/cn/glm-5.3-flash", Name: "GLM-5.3-Flash", Provider: provider.WorkBuddy, Context: 1000000, MaxOutput: 131072}, {ID: "qoder/cn/gfmodel", Name: "GLM-5.3-Flash", Provider: provider.Qoder, Source: "upstream"}}
	policies := map[string]config.ModelPolicy{}
	for _, model := range models {
		p, ok := RecommendClientPolicy(model)
		if !ok {
			t.Fatal(model.ID)
		}
		policies[model.ID] = p
	}
	lookup := func(id string) (config.ModelPolicy, bool) { p, ok := policies[id]; return p, ok }
	p, ok := config.CommonModelPolicy([]string{models[0].ID, models[1].ID}, lookup)
	if !ok || p.ContextWindow != 32768 || p.MaxOutputTokens != 4096 {
		t.Fatal("shared recommendation ignored smaller source", p)
	}
	if _, ok := config.CommonModelPolicy([]string{models[0].ID, "unknown"}, lookup); ok {
		t.Fatal("unknown source gained official/provider limits")
	}
}
