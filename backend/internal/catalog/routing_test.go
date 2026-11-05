package catalog

import (
	"errors"
	"reflect"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
)

type routingState struct {
	scope      access.AccessScope
	settings   config.Settings
	accounts   []*accounts.Account
	calls      []string
	scopeError error
}

func (s *routingState) AccessScope(key string) (access.AccessScope, error) {
	s.calls = append(s.calls, "scope:"+key)
	return s.scope, s.scopeError
}
func (s *routingState) Accounts() []*accounts.Account {
	s.calls = append(s.calls, "accounts")
	return s.accounts
}
func (s *routingState) Settings() config.Settings {
	s.calls = append(s.calls, "settings")
	return s.settings
}
func routingFixture() (*Routing, *routingState) {
	st := &routingState{accounts: []*accounts.Account{
		{ID: "wb", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true},
		{ID: "trae", Provider: provider.Trae, Scope: "cn", Enabled: true},
	}}
	directory := registry.New()
	directory.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "deepseek-v4.1-flash"}})
	directory.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "DeepSeek-V4.1-Flash"}})
	st.scope = access.AccessScope{Restricted: true, AccountIDs: map[string]bool{"wb": true, "trae": true}, ModelMappings: []access.ModelMapping{{Name: "deepseek-4.1-flash", Sources: []string{"workbuddy/cn/deepseek-v4.1-flash", "trae/cn/DeepSeek-V4.1-Flash"}}}}
	st.settings.DefaultModel = "deepseek-4.1-flash"
	return NewRouting(st, directory), st
}

func TestMappingImageCapabilityRequiresAllSources(t *testing.T) {
	for _, tc := range []struct{ a, b, want string }{{"supported", "supported", "supported"}, {"supported", "unsupported", "unsupported"}, {"supported", "", ""}, {"", "", ""}} {
		p := &ModelPlan{Public: "vision-1.0", IDs: []string{"a", "b"}, Models: map[string]provider.Model{"a": {ImageInput: tc.a}, "b": {ImageInput: tc.b}}}
		if m := p.PublicModel(); m.ImageInput != tc.want {
			t.Fatal("mapping overstated capabilities", m)
		}
	}
}

func TestRoutingMappedVisibilitySelectionAndReauthorization(t *testing.T) {
	r, st := routingFixture()
	list, err := r.VisibleModels("key")
	if err != nil || len(list) != 1 || list[0].ID != "deepseek-4.1-flash" || list[0].Source != "mapping" {
		t.Fatal(list, err)
	}
	model, plan, err := r.Resolve("key", st.settings, " ")
	if err != nil || model.ID != list[0].ID || len(plan.IDs) != 2 {
		t.Fatal(model, plan, err)
	}
	if _, err := plan.Selected(plan.IDs[0], provider.Trae); err == nil {
		t.Fatal("cross-platform selection accepted")
	}
	if err := plan.FilterMessages(true); err != nil || !reflect.DeepEqual(plan.IDs, []string{"workbuddy/cn/deepseek-v4.1-flash"}) {
		t.Fatal(plan, err)
	}
	// Filtering belongs only to this request, never to the shared mapping.
	_, other, err := r.Resolve("key", st.settings, model.ID)
	if err != nil || len(other.IDs) != 2 {
		t.Fatal("candidate filtering leaked", other, err)
	}
	fallbackCalls := 0
	fallback := func() bool { fallbackCalls++; return true }
	if err := r.Check("key", plan.IDs[0], false, plan, fallback); err != nil || fallbackCalls != 0 {
		t.Fatal("eager fallback", err)
	}
	// Removing the live mapping wins over a previously authorized plan/fallback.
	st.scope.ModelMappings = nil
	if err := r.Check("key", plan.IDs[0], false, plan, fallback); err == nil || fallbackCalls != 0 {
		t.Fatal("stale plan bypassed live authorization")
	}
	st.scope.AllowlistEnabled = true
	st.scope.ModelIDs = map[string]bool{}
	if _, _, err := r.Resolve("key", st.settings, plan.IDs[0]); err == nil {
		t.Fatal("raw ID bypassed allowlist")
	}
}

func TestRoutingResolutionOrderingAndMissingVisibility(t *testing.T) {
	r, st := routingFixture()
	settings := st.settings
	settings.ForceModel = "missing"
	_, _, err := r.Resolve("key", settings, "workbuddy/cn/deepseek-v4.1-flash")
	var upstream *provider.Error
	if !errors.As(err, &upstream) || upstream.Status != 400 {
		t.Fatal("force conflict was not first", err)
	}
	st.scopeError = errors.New("deleted key")
	_, _, err = r.Resolve("key", settings, "missing")
	if !errors.As(err, &upstream) || upstream.Status != 403 {
		t.Fatal("key authorization was not first", err)
	}
	st.scopeError = nil
	st.scope.ModelMappings = nil
	st.scope.AccountIDs = map[string]bool{"wb": true}
	st.calls = nil
	_, _, err = r.Resolve("key", st.settings, "missing")
	var missing *provider.ModelNotFound
	if !errors.As(err, &missing) || !reflect.DeepEqual(missing.Available, []string{"workbuddy/cn/deepseek-v4.1-flash"}) {
		t.Fatal("missing error leaked other groups", err)
	}
	if !reflect.DeepEqual(st.calls, []string{"scope:key", "scope:key", "settings", "accounts"}) {
		t.Fatal("snapshot order changed", st.calls)
	}
	// Explicit platform/region cannot silently resolve to another provider.
	settings = config.Settings{ModelAliases: map[string]string{"workbuddy/cn/alias": "trae/cn/DeepSeek-V4.1-Flash"}}
	if _, err := r.ResolveDefault(settings, "workbuddy/cn/alias"); !errors.As(err, &upstream) || upstream.Status != 400 {
		t.Fatal(err)
	}
}

func TestRoutingUnavailableSourcesAndSearchIsolation(t *testing.T) {
	r, st := routingFixture()
	st.scope.AllowlistEnabled = true
	st.scope.ModelIDs = map[string]bool{}
	if _, _, err := r.Resolve("key", st.settings, "deepseek-4.1-flash"); !errors.Is(err, accounts.ErrNoAccount) {
		t.Fatal(err)
	}
	st.scope.ModelIDs["workbuddy/cn/web-search"] = true
	if err := r.Check("key", "workbuddy/cn/web-search", true, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"workbuddy/cn/arbitrary-label", "trae/global/web-search", "codearts/not-search", "other/cn/web-search"} {
		if err := r.Check("key", id, true, nil, nil); err == nil {
			t.Fatal("invalid search identifier authorized", id)
		}
	}
	st.scope.Restricted = false
	st.calls = nil
	if err := r.Check("key", "unknown", false, &ModelPlan{}, nil); err != nil || !reflect.DeepEqual(st.calls, []string{"scope:key"}) {
		t.Fatal("unrestricted short circuit changed", err, st.calls)
	}
}
