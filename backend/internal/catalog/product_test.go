package catalog

import (
	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"errors"
	"reflect"
	"testing"
)

func flag(v bool) *bool { return &v }
func TestProductDefaultsAndIndependentVisibility(t *testing.T) {
	r, st := routingFixture()
	st.scope.DefaultModel = "deepseek-4.1-flash"
	st.settings.DefaultModel = "wrong-global"
	_, plan, err := r.Resolve("key", st.settings, "")
	if err != nil || plan == nil {
		t.Fatal(err)
	}
	if _, _, err = r.Resolve("key", st.settings, "typo-4.1"); err == nil {
		t.Fatal("unknown name rescued by default")
	}
	st.scope.ModelRules = []access.ModelRule{{ID: plan.Public, Visible: flag(false)}}
	list, _ := r.VisibleModels("key")
	if len(list) != 0 {
		t.Fatal(list)
	}
	if _, _, err = r.Resolve("key", st.settings, plan.Public); err != nil {
		t.Fatal("hidden is callable", err)
	}
	st.scope.ModelRules[0].Visible = flag(true)
	st.scope.ModelRules[0].Callable = flag(false)
	list, _ = r.VisibleModels("key")
	if len(list) != 1 {
		t.Fatal("disabled may stay visible", list)
	}
	if _, _, err = r.Resolve("key", st.settings, plan.Public); err == nil {
		t.Fatal("disabled public call")
	}
	if err = r.Check("key", plan.IDs[0], false, plan, func() bool { return true }); err == nil {
		t.Fatal("stale plan bypassed public revoke")
	}
	if _, _, err = r.Resolve("key", st.settings, plan.IDs[0]); err != nil {
		t.Fatal(err)
	}
	st.scope.ModelRules = []access.ModelRule{{ID: plan.IDs[0], Visible: flag(true), Callable: flag(false)}}
	_, narrowed, err := r.Resolve("key", st.settings, plan.Public)
	if err != nil || len(narrowed.IDs) != 1 || narrowed.IDs[0] == plan.IDs[0] {
		t.Fatal(narrowed, err)
	}
	if _, _, err = r.Resolve("key", st.settings, plan.IDs[0]); err == nil {
		t.Fatal("disabled raw call")
	}
	list, _ = r.VisibleModels("key")
	if len(list) != 2 {
		t.Fatal("explicit raw listing", list)
	}
	st.scope.ClientDefaultModel = plan.Public
	if got := r.ClientDefault("key"); got != plan.Public {
		t.Fatal(got)
	}
	st.scope.ModelRules = append(st.scope.ModelRules, access.ModelRule{ID: plan.Public, Visible: flag(false)})
	if got := r.ClientDefault("key"); got != "" {
		t.Fatal("hidden client preference", got)
	}
}
func TestProductVisibleNativeModelIsDirectCallable(t *testing.T) {
	r, st := routingFixture()
	st.accounts = append(st.accounts, &accounts.Account{ID: "native", Enabled: true})
	st.scope.AccountIDs["native"] = true
	st.scope.ModelRules = []access.ModelRule{{ID: "deepseek-v4.1-flash", Visible: flag(true)}}
	list, _ := r.VisibleModels("key")
	found := false
	for _, m := range list {
		if m.ID == "codearts/deepseek-v4.1-flash" {
			found = true
			if _, plan, err := r.Resolve("key", st.settings, m.ID); err != nil || plan != nil {
				t.Fatal("listed physical name not callable", m, err)
			}
		}
	}
	if !found {
		t.Fatal(list)
	}
}

func TestProductLegacyDefaultsForceAndPresetIsolation(t *testing.T) {
	r, st := routingFixture()
	st.scope.ModelMappings = nil
	st.settings.DefaultModel = "workbuddy/cn/deepseek-v4.1-flash"
	if _, plan, err := r.Resolve("key", st.settings, ""); err != nil || plan != nil {
		t.Fatal(err)
	}
	st.scope.DefaultModel = "trae/cn/DeepSeek-V4.1-Flash"
	st.settings.ForceModel = st.settings.DefaultModel
	m, _, err := r.Resolve("key", st.settings, "")
	if err != nil || m.ID != st.settings.ForceModel {
		t.Fatal("force precedence changed", m, err)
	}
	st.settings.ForceModel = ""
	st.scope.DefaultModel = ""
	_, _, err = r.Resolve("key", st.settings, "wrong")
	var missing *provider.ModelNotFound
	if !errors.As(err, &missing) {
		t.Fatal(err)
	}
	r.directory.(*registry.Registry).ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "deepseek-v4.1-flash"}})
	before := st.scope
	p := r.Presets()
	if p.Version < 1 || len(p.Mappings) == 0 {
		t.Fatal(p)
	}
	available := []string{}
	for _, s := range p.Mappings[0].Sources {
		if s.Available {
			available = append(available, s.ID)
		}
	}
	if !reflect.DeepEqual(available, []string{"deepseek-v4.1-flash", "workbuddy/cn/deepseek-v4.1-flash", "trae/cn/deepseek-v4.1-flash"}) {
		t.Fatal(available)
	}
	p.Mappings[0].Sources[0].ID = "corrupted"
	if r.Presets().Mappings[0].Sources[0].ID == "corrupted" || !reflect.DeepEqual(st.scope, before) {
		t.Fatal("preset modified shared state")
	}
	directory := r.directory.(*registry.Registry)
	directory.ReplaceProvider(provider.WorkBuddy, "cn", nil)
	if r.Presets().Mappings[0].Sources[1].Available {
		t.Fatal("missing source still reported available")
	}
}
