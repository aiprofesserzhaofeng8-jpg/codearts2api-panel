package catalog

import (
	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"errors"
	"testing"
)

type unifiedRoutingFixture struct {
	*routingState
	snapshot config.UnifiedSnapshot
}

func (s *unifiedRoutingFixture) UnifiedSnapshot() config.UnifiedSnapshot { return s.snapshot }
func unifiedRouter() (*Routing, *unifiedRoutingFixture) {
	base, old := routingFixture()
	m := config.UnifiedModel{ID: "um_11111111111111111111111111111111", Name: "deepseek-4.1-flash", Enabled: true, Revision: 1, Sources: []config.ModelSource{{ID: "workbuddy/cn/deepseek-v4.1-flash", Enabled: true}, {ID: "trae/cn/DeepSeek-V4.1-Flash", Enabled: true}}}
	s := &unifiedRoutingFixture{routingState: old, snapshot: config.UnifiedSnapshot{Revision: 1, Models: []config.UnifiedModel{m}}}
	s.scope = access.AccessScope{}
	return NewRouting(s, base.directory), s
}
func TestUnifiedResolveUsesGlobalDefinitionsAndExactNames(t *testing.T) {
	r, s := unifiedRouter()
	m, p, err := r.Resolve("key", s.settings, "deepseek-4.1-flash")
	if err != nil || m.ID != "deepseek-4.1-flash" || len(p.IDs) != 2 || p.Selection.ID == "" {
		t.Fatal(m, p, err)
	}
	_, _, err = r.Resolve("key", s.settings, "Deepseek-4.1-flash")
	var missing *provider.ModelNotFound
	if !errors.As(err, &missing) {
		t.Fatal("case fell through", err)
	}
	s.scope = access.AccessScope{Restricted: true, RawModelMode: "qualified_only"}
	_, _, err = r.Resolve("key", s.settings, m.ID)
	var denied *config.ModelError
	if !errors.As(err, &denied) || denied.Status != 403 {
		t.Fatal(err)
	}
	_, _, err = r.Resolve("key", s.settings, "glm-5.3")
	if !errors.As(err, &missing) {
		t.Fatal("raw bare escaped", err)
	}
}
func TestUnifiedCapabilityFiltersDoNotModifyRequest(t *testing.T) {
	r, s := unifiedRouter()
	dir := r.directory.(*registry.Registry)
	dir.ReplaceProvider("workbuddy", "cn", []provider.Model{{ID: "deepseek-v4.1-flash", ImageInput: "supported", ToolCalling: "supported", MaxOutput: 512}})
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}],"tools":[{"type":"function","function":{"name":"f","parameters":{"const":9007199254740993}}}],"max_tokens":128}`)
	before := string(body)
	_, p, err := r.Resolve("key", s.settings, "deepseek-4.1-flash")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.FilterCapabilities(body); err != nil || len(p.IDs) != 1 || p.IDs[0] != "workbuddy/cn/deepseek-v4.1-flash" {
		t.Fatal(p, err)
	}
	if string(body) != before {
		t.Fatal("mutated request")
	}
	_, p, _ = r.Resolve("key", s.settings, "deepseek-4.1-flash")
	if err = p.FilterCapabilities([]byte(`{"tools":[{}],"max_tokens":1024}`)); err == nil {
		t.Fatal("exceeded known output")
	}
	_, p, _ = r.Resolve("key", s.settings, "deepseek-4.1-flash")
	if len(p.IDs) != 2 {
		t.Fatal("request filter leaked")
	}
}
func TestUnifiedCheckRevalidatesUngroupedRevision(t *testing.T) {
	r, s := unifiedRouter()
	_, p, _ := r.Resolve("key", s.settings, "deepseek-4.1-flash")
	s.snapshot.Revision++
	err := r.Check("key", p.IDs[0], false, p, func() bool { return true })
	var conflict *config.ModelError
	if !errors.As(err, &conflict) || conflict.Status != 409 {
		t.Fatal(err)
	}
}

func TestUnifiedNativeNameCollisionPreservesLegacyButNotExplicitDenial(t *testing.T) {
	r, s := unifiedRouter()
	s.snapshot.Models[0].Name = "deepseek-v4.1-flash"
	s.snapshot.Models[0].Sources = append(s.snapshot.Models[0].Sources, config.ModelSource{ID: "codearts/deepseek-v4.1-flash", Enabled: true})
	s.accounts = append(s.accounts, &accounts.Account{ID: "native", Provider: provider.CodeArts, Enabled: true})
	s.scope = access.AccessScope{Restricted: true, RawModelMode: "legacy", AccountIDs: map[string]bool{"native": true}, AllowlistEnabled: true, ModelIDs: map[string]bool{"deepseek-v4.1-flash": true}}
	m, plan, err := r.Resolve("key", s.settings, "deepseek-v4.1-flash")
	if err != nil || plan != nil || ModelProvider(m) != provider.CodeArts {
		t.Fatal("legacy route replaced", m, plan, err)
	}
	s.scope.UnifiedModels = []access.UnifiedGrant{{ModelID: s.snapshot.Models[0].ID, Callable: false, Visible: true, SourceMode: "all", SourceIDs: []string{}}}
	_, _, err = r.Resolve("key", s.settings, "deepseek-v4.1-flash")
	var denied *config.ModelError
	if !errors.As(err, &denied) || denied.Status != 403 {
		t.Fatal("explicit deny bypassed", err)
	}
	_, plan, err = r.Resolve("key", s.settings, "codearts/deepseek-v4.1-flash")
	if err != nil || plan != nil {
		t.Fatal("qualified raw blocked", err)
	}
	s.scope = access.AccessScope{}
	_, plan, err = r.Resolve("key", s.settings, "deepseek-v4.1-flash")
	if err != nil || plan == nil || len(plan.IDs) != 3 {
		t.Fatal("public sources unavailable", plan, err)
	}
}

func TestUnifiedCapabilityUnionRequiresSameSourceForImageAndTools(t *testing.T) {
	r, s := unifiedRouter()
	dir := r.directory.(*registry.Registry)
	dir.ReplaceProvider("workbuddy", "cn", []provider.Model{{ID: "deepseek-v4.1-flash", ImageInput: "supported", ToolCalling: "unsupported"}})
	dir.ReplaceProvider("trae", "cn", []provider.Model{{ID: "DeepSeek-V4.1-Flash", ImageInput: "unsupported", ToolCalling: "supported"}})
	model, p, err := r.Resolve("key", s.settings, s.snapshot.Models[0].Name)
	if err != nil || model.ImageInput != "supported" || model.ToolCalling != "supported" {
		t.Fatal(model, err)
	}
	image := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}]}`)
	if err = p.FilterCapabilities(image); err != nil || len(p.IDs) != 1 || p.IDs[0] != "workbuddy/cn/deepseek-v4.1-flash" {
		t.Fatal(p, err)
	}
	_, p, _ = r.Resolve("key", s.settings, s.snapshot.Models[0].Name)
	both := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}],"tools":[{"type":"function","function":{"name":"read"}}]}`)
	if err = p.FilterCapabilities(both); err == nil {
		t.Fatal("capabilities from two different sources combined")
	}
	s.scope = access.AccessScope{Restricted: true, UnifiedModels: []access.UnifiedGrant{{ModelID: s.snapshot.Models[0].ID, Callable: true, Visible: true, SourceMode: "selected", SourceIDs: []string{"trae/cn/DeepSeek-V4.1-Flash"}}}}
	model, _, err = r.Resolve("key", s.settings, s.snapshot.Models[0].Name)
	if err != nil || model.ImageInput != "unsupported" {
		t.Fatal("unauthorized image source leaked", model, err)
	}
}
