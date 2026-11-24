package catalog

import (
	"codearts2api/internal/registry"
	"codearts2api/internal/storage"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog/modelmeta"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func TestClientReferenceAliasesReachClientPolicies(t *testing.T) {
	// Selected unmodified limit records from https://models.dev/api.json,
	// captured 2026-11-23. No upstream calls or real credentials are needed.
	body, err := os.ReadFile("testdata/client-reference-limits-20261123.json")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := modelmeta.Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{references: registry}
	for _, tc := range []struct {
		platform, scope, id string
		context, output     int
	}{
		{provider.CodeArts, "", "openpangu-2.0-flash", 524288, 131072},
		{provider.CodeArts, "", "openpangu-2.0-pro", 524288, 131072},
		{provider.CodeArts, "", "deepseek-v4.1-flash-1017", 1000000, 384000},
		{provider.Trae, "cn", "DeepSeek-V4-Flash-Official", 1000000, 384000},
		{provider.Trae, "cn", "DeepSeek-V4-Pro-Official", 1000000, 384000},
		{provider.Trae, "cn", "Doubao-Seed-2.0-Code", 262144, 131072},
		{provider.Trae, "cn", "Doubao-Seed-2.1-Pro", 1024000, 256000},
		{provider.Trae, "cn", "Doubao-Seed-2.1-Turbo", 256000, 256000},
		{provider.Trae, "cn", "qwen-3.7-plus", 1000000, 65536},
	} {
		t.Run(tc.id, func(t *testing.T) {
			models := []provider.Model{{ID: tc.id, Name: tc.id, Source: "upstream", Context: 32768, MaxOutput: 4096}}
			m := r.withClientReferences(tc.platform, tc.scope, models)[0]
			ref := m.ClientReference
			if ref == nil || ref.ContextWindow != tc.context || ref.MaxOutputTokens != tc.output {
				t.Fatal("registry limits not matched", ref)
			}
			if models[0].ClientReference != nil || m.ID != tc.id || m.Context != 32768 || m.MaxOutput != 4096 {
				t.Fatal("reference changed routing or provider observations", m)
			}
			m.ID, m.Provider, m.Scope = provider.ModelID(tc.platform, tc.scope, tc.id), tc.platform, tc.scope
			p, ok, evidence := sourceClientPolicy(m, nil)
			// The existing summary-policy contract reserves 4096 tokens plus
			// a 1024-token minimum input when output equals the whole window.
			if !ok || p.ContextWindow != tc.context || p.MaxOutputTokens != min(tc.output, tc.context-5120) || evidence.Basis != "public_registry" {
				t.Fatal("client still received compatibility defaults", p, evidence)
			}
			if got := SourceClientCompatibility(m, &p, nil); !got.Exportable {
				t.Fatal("matched chat model cannot be exported", got)
			}
			override := p
			override.ContextWindow += 1024
			got, _, evidence := sourceClientPolicy(m, map[string]config.ModelPolicy{m.ID: override})
			if got != override || evidence.Basis != "administrator_config" {
				t.Fatal("reference overwrote administrator policy", got, evidence)
			}
		})
	}
	for _, id := range []string{"custom_model_doubao_1M", "Doubao-Seed-2.1-Pro-unknown", "deepseek-v4-pro-unknown", "auto", "glm-5.2-sft-harmony", "openpangu-2.1-pro", "speed"} {
		m := r.withClientReferences(provider.Trae, "cn", []provider.Model{{ID: id, Name: id}})[0]
		if m.ClientReference != nil {
			t.Fatal("unlisted model inherited another model's limits", m)
		}
	}
	// An exact registry entry takes precedence if the upstream adds it later.
	r.references, _ = modelmeta.Parse([]byte(`{"opencode":{"models":{"qwen-3.7-plus":{"limit":{"context":900000,"output":32000}},"qwen3.7-plus":{"limit":{"context":1000000,"output":65536}}}}}`))
	m := r.withClientReferences(provider.Trae, "cn", []provider.Model{{ID: "qwen-3.7-plus", Name: "Qwen"}})[0]
	if m.ClientReference == nil || m.ClientReference.ContextWindow != 900000 {
		t.Fatal("alias overrode exact registry match", m)
	}
}

func TestReferencesEnrichOnlyExistingModelsAndAreCachedOffInferencePath(t *testing.T) {
	r, st, directory := runtimeFixture(t, &accounts.Account{ID: "wb", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true})
	before := st.ModelPolicies()
	calls := 0
	r.loadReferences = func(ctx context.Context) ([]byte, error) {
		calls++
		if d, ok := ctx.Deadline(); !ok || time.Until(d) > 5*time.Second {
			t.Error("reference request has no short deadline")
		}
		return []byte(`{"opencode":{"models":{"glm-5.3":{"limit":{"context":1000000,"output":131072}}}},"zhipuai":{"models":{"glm-5.3":{"id":"glm-5.3","limit":{"context":131072,"output":16384},"modalities":{"input":["text","image"]}},"future-model":{"limit":{"context":1000000}}}}}`), nil
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
	r.BindRefreshContext(t.Context())
	r.SignalIfStale(time.Minute, ops)
	if calls != 1 {
		t.Fatal("fresh catalog called registry")
	}
	m, _ := directory.ResolveRaw("workbuddy/cn/glm-5.3")
	if m.Context != 16384 || m.MaxOutput != 0 || m.ImageInput != "unsupported" || m.ToolCalling != "" || m.ClientReference == nil {
		t.Fatal("public reference altered raw upstream limits/capabilities", m)
	}
	p, ok, evidence := sourceClientPolicy(m, nil)
	if !ok || p.ContextWindow != 1000000 || p.MaxOutputTokens != 131072 || evidence.Basis != "public_registry" || evidence.ProviderMaxOutput != 0 {
		t.Fatal("published limits or provenance lost", p, evidence)
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
	m := provider.Model{ID: "teleagent/cn/chat-unlisted", Name: "Unlisted Chat", Provider: provider.TeleAgent, Source: "upstream"}
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

func TestRefreshRetainsSameSourceReferenceAcrossPartialUpdates(t *testing.T) {
	r, _, directory := runtimeFixture(t, &accounts.Account{ID: "wb", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true})
	models := []provider.Model{{ID: "glm-5.3", Name: "GLM-5.3", Source: "upstream"}}
	for _, tc := range []struct {
		limits         string
		window, output int
	}{
		{`{"context":900000,"output":32768}`, 900000, 32768},
		{"", 900000, 32768},
		{`{"output":65536}`, 900000, 65536},
		{`{"context":800000}`, 800000, 65536},
	} {
		r.references = nil
		if tc.limits != "" {
			var err error
			r.references, err = modelmeta.Parse([]byte(`{"opencode":{"models":{"glm-5.3":{"limit":` + tc.limits + `}}}}`))
			if err != nil {
				t.Fatal(err)
			}
		}
		result := r.withClientReferences(provider.WorkBuddy, "cn", models)
		ref := result[0].ClientReference
		if ref == nil || ref.ContextWindow != tc.window || ref.MaxOutputTokens != tc.output {
			t.Fatalf("limits %s: got %+v", tc.limits, ref)
		}
		directory.ReplaceProvider(provider.WorkBuddy, "cn", result)
	}
	r.references = nil
	models[0].Name = "Replacement model"
	if r.withClientReferences(provider.WorkBuddy, "cn", models)[0].ClientReference != nil {
		t.Fatal("replacement inherited old reference")
	}
}

func TestClientReferenceSurvivesRestartAndOfflineRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	open := func() (*storage.Store, *Runtime, *registry.Registry) {
		t.Helper()
		st, err := storage.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		dir := registry.New()
		st.RestoreCatalogPartitions(func(p, s string, models []provider.Model, _ time.Time) { dir.ReplaceProvider(p, s, models) })
		return st, NewRuntime(st, dir), dir
	}
	st, r, _ := open()
	st.UpsertAccount(&accounts.Account{ID: "wb", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true})
	ops := RefreshOperations{Provider: func(context.Context, *accounts.Account) ([]provider.Model, error) {
		return []provider.Model{{ID: "glm-5.3", Name: "GLM-5.3", Source: "upstream"}}, nil
	}}
	network := func(body string) func(context.Context) ([]byte, error) {
		return func(context.Context) ([]byte, error) {
			if body == "" {
				return nil, errors.New("offline")
			}
			return []byte(body), nil
		}
	}
	check := func(dir *registry.Registry, window, output int) {
		t.Helper()
		m, ok := dir.ResolveRaw("workbuddy/cn/glm-5.3")
		p, ready := RecommendClientPolicy(m)
		if !ok || !ready || p.ContextWindow != window || p.MaxOutputTokens != output {
			t.Fatal("restored recommendation", ok, ready, p)
		}
	}
	r.loadReferences = network(`{"opencode":{"models":{"glm-5.3":{"limit":{"context":900000,"output":32768}}}}}`)
	if err := r.Refresh(t.Context(), ops); err != nil {
		t.Fatal(err)
	}
	_, r, dir := open()
	check(dir, 900000, 32768)
	r.loadReferences = network("")
	if err := r.Refresh(t.Context(), ops); err != nil {
		t.Fatal(err)
	}
	_, r, dir = open()
	check(dir, 900000, 32768)
	r.loadReferences = network(`{"opencode":{"models":{"glm-5.3":{"limit":{"output":65536}}}}}`)
	if err := r.Refresh(t.Context(), ops); err != nil {
		t.Fatal(err)
	}
	_, _, dir = open()
	check(dir, 900000, 65536)
}

func TestBundledReferenceOfflineAndPrecedence(t *testing.T) {
	r, _, directory := runtimeFixture(t)
	models := []provider.Model{{ID: "glm-5.3", Name: "GLM-5.3", Source: "upstream"}}
	got := r.withClientReferences(provider.WorkBuddy, "cn", models)[0]
	if got.ClientReference == nil || got.ClientReference.ContextWindow != 1000000 || got.ClientReference.MaxOutputTokens != 131072 {
		t.Fatal("fresh offline discovery did not use bundled reference", got.ClientReference)
	}
	if models[0].ClientReference != nil {
		t.Fatal("reference enrichment mutated provider input")
	}
	got.ClientReference = &provider.ClientReference{ModelID: "glm-5.3", ContextWindow: 900000, MaxOutputTokens: 64000}
	directory.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{got})
	retained := r.withClientReferences(provider.WorkBuddy, "cn", models)[0]
	if retained.ClientReference.ContextWindow != 900000 {
		t.Fatal("bundled snapshot overwrote persisted reference", retained.ClientReference)
	}
	r.references, _ = modelmeta.Parse([]byte(`{"opencode":{"models":{"glm-5.3":{"limit":{"context":950000,"output":65000}}}}}`))
	fresh := r.withClientReferences(provider.WorkBuddy, "cn", models)[0]
	if fresh.ClientReference.ContextWindow != 950000 {
		t.Fatal("online reference did not override bundled or persisted value", fresh.ClientReference)
	}
	fresh.ID = "workbuddy/cn/glm-5.3"
	policy, ok := RecommendClientPolicy(fresh)
	if !ok {
		t.Fatal("reference did not reach client recommendation")
	}
	policy.ContextWindow = 800000
	actual, _, evidence := sourceClientPolicy(fresh, map[string]config.ModelPolicy{fresh.ID: policy})
	if actual.ContextWindow != 800000 || evidence.Basis != "administrator_config" {
		t.Fatal("reference overwrote administrator policy")
	}
	unknown := r.withClientReferences(provider.Trae, "cn", []provider.Model{{ID: "custom_model_1M", Name: "custom_model_1M"}})[0]
	if unknown.ClientReference != nil {
		t.Fatal("bundled reference guessed an internal model identity")
	}
}
