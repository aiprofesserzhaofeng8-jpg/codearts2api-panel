package catalog

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func TestReferencesEnrichOnlyExistingModelsAndAreCachedOffInferencePath(t *testing.T) {
	r, st, directory := runtimeFixture(t, &accounts.Account{ID: "wb", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true})
	before := st.ModelPolicies()
	calls := 0
	r.loadReferences = func(ctx context.Context) ([]byte, error) {
		calls++
		if d, ok := ctx.Deadline(); !ok || time.Until(d) > 5*time.Second {
			t.Error("reference request has no short deadline")
		}
		return []byte(`{"zhipuai":{"models":{"glm-5.3":{"id":"glm-5.3","limit":{"context":131072,"output":16384},"modalities":{"input":["text","image"]}},"future-model":{"limit":{"context":1000000}}}}}`), nil
	}
	ops := RefreshOperations{Provider: func(context.Context, *accounts.Account) ([]provider.Model, error) {
		return []provider.Model{{ID: "glm-5.3", Name: "GLM-5.3", Context: 16384, Source: "upstream", ImageInput: "unsupported"}, {ID: "auto", Name: "Automatic", Source: "upstream"}}, nil
	}}
	for range 2 {
		if err := r.Refresh(context.Background(), ops); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || !reflect.DeepEqual(before, st.ModelPolicies()) {
		t.Fatal("reference refresh bypassed TTL or changed persistent policies", calls)
	}
	r.RefreshIfStale(time.Minute, ops)
	if calls != 1 {
		t.Fatal("fresh catalog called registry")
	}
	m, _ := directory.ResolveRaw("workbuddy/cn/glm-5.3")
	if m.Context != 16384 || m.MaxOutput != 0 || m.ImageInput != "unsupported" || m.ToolCalling != "" || m.ClientReference == nil {
		t.Fatal("public reference altered raw upstream limits/capabilities", m)
	}
	p, ok, evidence := sourceClientPolicy(m, nil)
	if !ok || p.ContextWindow != 16384 || p.MaxOutputTokens != 4096 || evidence.Basis != "public_registry" || evidence.ProviderMaxOutput != 0 {
		t.Fatal("missing-only merge or provenance failed", p, evidence)
	}
	if _, ok := directory.ResolveRaw("workbuddy/cn/future-model"); ok {
		t.Fatal("public registry added an unavailable gateway model")
	}
	auto, _ := directory.ResolveRaw("workbuddy/cn/auto")
	_, ok, evidence = sourceClientPolicy(auto, nil)
	if !ok || auto.ClientReference != nil || evidence.Basis != "compatibility_default" {
		t.Fatal("auto route inherited a guessed native identity", auto, evidence)
	}
	// Optional refresh failure preserves old references and model availability.
	r.referenceRetry = time.Time{}
	r.loadReferences = func(context.Context) ([]byte, error) { calls++; return nil, errors.New("offline") }
	for range 2 {
		if err := r.Refresh(context.Background(), ops); err != nil {
			t.Fatal("optional reference failure blocked discovery", err)
		}
	}
	m, _ = directory.ResolveRaw("workbuddy/cn/glm-5.3")
	if calls != 2 || m.ClientReference == nil {
		t.Fatal("failed references retried immediately or erased cache", calls)
	}
}

func TestClientDefaultsPreserveOverridesAndDistinguishMissingSources(t *testing.T) {
	m := provider.Model{ID: "teleagent/cn/chat-pro", Name: "Chat Pro", Provider: provider.TeleAgent, Source: "upstream"}
	p, ok, evidence := sourceClientPolicy(m, nil)
	if !ok || p.ContextWindow != 32768 || p.MaxOutputTokens != 4096 || evidence.Basis != "compatibility_default" || p.SummaryEnabled || evidence.ProviderContext != 0 {
		t.Fatal("unknown limits require manual certification", p, evidence)
	}
	compat := clientCompatibility([]provider.Model{m}, &p, nil)
	if !compat.Exportable || compat.Reason == "" || compat.Status != "ready" {
		t.Fatal("defaults were not identified as defaults", compat)
	}
	override := p
	override.ContextWindow = 65536
	got, ok, evidence := sourceClientPolicy(m, map[string]config.ModelPolicy{m.ID: override})
	if !ok || got.ContextWindow != override.ContextWindow || evidence.Basis != "administrator_config" {
		t.Fatal("default overwrote administrator policy", got, evidence)
	}
	if _, ok := RecommendClientPolicy(provider.Model{ID: m.ID}); ok {
		t.Fatal("missing directory placeholder acquired defaults")
	}
	// Provider bounds always win, even when too small for our client policy.
	m.Context, m.MaxOutput = 4096, 128
	if _, ok := RecommendClientPolicy(m); ok {
		t.Fatal("defaults raised explicitly incompatible provider bounds")
	}
}

func TestDefaultModelPreviewRejectsChangedReferenceLimits(t *testing.T) {
	_, st, directory := runtimeFixture(t)
	policies := config.NewModelPolicies(st, directory, RecommendClientPolicy)
	management := NewManagement(st, directory, policies)
	m := provider.Model{ID: "glm-5.3", Name: "GLM-5.3", Source: "upstream", ClientReference: &provider.ClientReference{ContextWindow: 65536, MaxOutputTokens: 8192}}
	directory.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{m})
	preview, err := management.DefaultModels()
	if err != nil {
		t.Fatal(err)
	}
	// Only public reference limits change; upstream fields and routing stay put.
	m.ClientReference = &provider.ClientReference{ContextWindow: 131072, MaxOutputTokens: 8192}
	directory.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{m})
	_, err = management.ApplyDefaultModels(config.DefaultModelsApply{Version: preview.Version, CatalogRevision: preview.CatalogRevision, ExpectedRoutingRevision: preview.RoutingRevision, Names: []string{"glm-5.3"}})
	var problem *config.ModelError
	if !errors.As(err, &problem) || problem.Reason != "revision_conflict" {
		t.Fatal("stale preview saved changed client defaults", err)
	}
}
