package catalog

import (
	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"testing"
)

func TestPublicModelIdentityAcrossAllProvidersPreservesVersion(t *testing.T) {
	for _, p := range []string{"codearts", "workbuddy", "trae", "qoder", "kiro", "chatgpt", "claude", "teleagent"} {
		for _, id := range []string{"glm-5.3-flash", "kimi-k3.1-pro", "mimo-v2.6-pro", "claude-sonnet-4.5", "gpt-5.6-sol"} {
			name, _ := defaultModelName(provider.Model{ID: provider.ModelID(p, "region", id)})
			if name != id {
				t.Fatalf("%s: %s -> %s", p, id, name)
			}
		}
	}
	for _, id := range []string{"auto", "dfmodel", "chat-pro", "custom_model_gpt-5", "deepseek-v4.1-flash-1017", "glm-5.3-flashx"} {
		name, _ := defaultModelName(provider.Model{ID: "trae/cn/" + id})
		if id == "deepseek-v4.1-flash-1017" || id == "glm-5.3-flashx" {
			if name != id {
				t.Fatal("version suffix lost", name)
			}
		} else if name != "trae-"+id {
			t.Fatal("opaque route invented a model", name)
		}
	}
}
func TestCanonicalListHidesGeneratedNamesButKeepsOldCallsAndACL(t *testing.T) {
	r, s := unifiedRouter()
	original := s.snapshot.Models[0]
	original.Name = "workbuddy-deepseek-v4.1-flash"
	original.Sources = original.Sources[:1]
	canonical := config.CloneUnifiedModel(s.snapshot.Models[0])
	canonical.Name = "deepseek-v4.1-flash"
	canonical.ID = "um_22222222222222222222222222222222"
	s.snapshot.Models = []config.UnifiedModel{original, canonical}
	list := r.unifiedModelsForScope(s.snapshot, s.scope)
	found := map[string]bool{}
	for _, m := range list {
		found[m.ID] = true
	}
	if found[original.Name] || !found[canonical.Name] {
		t.Fatal("generated name still advertised", found)
	}
	_, oldPlan, err := r.Resolve("key", s.settings, original.Name)
	if err != nil || oldPlan == nil || len(oldPlan.IDs) != 1 {
		t.Fatal("legacy client broken", oldPlan, err)
	}
	_, newPlan, err := r.Resolve("key", s.settings, canonical.Name)
	if err != nil || newPlan == nil || len(newPlan.IDs) != 2 {
		t.Fatal("canonical did not aggregate sources", newPlan, err)
	}
	s.scope = access.AccessScope{Restricted: true, RawModelMode: "qualified_only", AccountIDs: map[string]bool{"wb": true, "trae": true}, UnifiedModels: []access.UnifiedGrant{{ModelID: original.ID, Visible: true, Callable: true, SourceMode: "all", SourceIDs: []string{}}}}
	list = r.unifiedModelsForScope(s.snapshot, s.scope)
	found = map[string]bool{}
	for _, m := range list {
		found[m.ID] = true
	}
	if !found[original.Name] || found[canonical.Name] {
		t.Fatal("grant changed by presentation", found)
	}
}
