package catalog

import (
	"codearts2api/internal/provider"
	"testing"
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
