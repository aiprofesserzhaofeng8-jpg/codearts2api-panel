package catalog

import (
	"errors"
	"testing"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
)

func TestQoderConfirmedCNAliasesRequireCurrentDisplayName(t *testing.T) {
	if len(defaultCatalog.QoderCNAliases) != 10 {
		t.Fatal("missing aliases")
	}
	for _, a := range defaultCatalog.QoderCNAliases {
		raw := provider.Model{ID: "qoder/cn/" + a.ID, Name: a.DisplayName}
		name, _ := defaultModelName(raw)
		if name != a.Name {
			t.Fatal(a.ID, name)
		}
		raw.Name = "Changed-model-v99"
		if !sourceIdentityChanged(a.Name, raw) || sourceIdentityChanged("qoder-"+a.ID, raw) {
			t.Fatal("identity check must constrain native names without blocking service entries", a.ID)
		}
		name, _ = defaultModelName(raw)
		if name != providerSpecificName(raw) {
			t.Fatal("changed catalog silently merged", a.ID, name)
		}
		raw.ID = "qoder/global/" + a.ID
		raw.Name = a.DisplayName
		name, _ = defaultModelName(raw)
		if name == a.Name {
			t.Fatal("CN evidence applied globally", a.ID)
		}
	}
	for _, id := range []string{"fast", "auto"} {
		name, _ := defaultModelName(provider.Model{ID: "qoder/cn/" + id, Name: id})
		if name != "qoder-"+id {
			t.Fatal(name)
		}
	}
}

func TestSavedNativeQoderBindingRejectsChangedSlot(t *testing.T) {
	r, s := unifiedRouter()
	dir := r.directory.(*registry.Registry)
	s.snapshot.Models[0].Name = "glm-5.3"
	s.snapshot.Models[0].Sources = []config.ModelSource{{ID: "qoder/cn/gmodel", Enabled: true}}
	dir.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gmodel", Name: "GLM-5.3", ToolCalling: "supported"}})
	if _, p, err := r.Resolve("key", s.settings, "glm-5.3"); err != nil || len(p.IDs) != 1 {
		t.Fatal(p, err)
	}
	dir.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gmodel", Name: "Different-native-v99", ToolCalling: "supported"}})
	_, _, err := r.Resolve("key", s.settings, "glm-5.3")
	var failure *config.ModelError
	if !errors.As(err, &failure) || failure.Reason != "model_source_identity_changed" {
		t.Fatal("saved binding kept changed identity", err)
	}
	p, err := r.buildUnifiedPlan(s.snapshot, s.scope, s.snapshot.Models[0], true)
	if err != nil || p.PublicModel().ToolCalling == "supported" {
		t.Fatal("profile kept stale tool guarantee", p, err)
	}
	// Explicit provider routes remain callable, and a restored slot recovers.
	if _, _, err := r.Resolve("key", s.settings, "qoder/cn/gmodel"); err != nil {
		t.Fatal("provider service blocked", err)
	}
	dir.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gmodel", Name: "GLM-5.3"}})
	if _, _, err := r.Resolve("key", s.settings, "glm-5.3"); err != nil {
		t.Fatal("restored slot did not recover", err)
	}
	// A changed slot must not take a matching alternative source out of service.
	dir.ReplaceProvider(provider.Qoder, "cn", []provider.Model{{ID: "gmodel", Name: "Different-native-v99"}})
	dir.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "glm-5.3", Name: "GLM-5.3"}})
	s.snapshot.Models[0].Sources = append(s.snapshot.Models[0].Sources, config.ModelSource{ID: "workbuddy/cn/glm-5.3", Enabled: true})
	_, p, err = r.Resolve("key", s.settings, "glm-5.3")
	if err != nil || len(p.IDs) != 1 || p.IDs[0] != "workbuddy/cn/glm-5.3" {
		t.Fatal("changed slot blocked a matching source", p, err)
	}
}
