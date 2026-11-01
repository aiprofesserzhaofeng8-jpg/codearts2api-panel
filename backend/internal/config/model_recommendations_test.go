package config

import (
	"reflect"
	"testing"

	"codearts2api/internal/provider"
)

type policyStore map[string]ModelPolicy

func (s policyStore) ModelPolicy(id string) (ModelPolicy, bool) { p, ok := s[id]; return p, ok }
func (s policyStore) ModelPolicies() map[string]ModelPolicy {
	out := make(map[string]ModelPolicy, len(s))
	for id, p := range s {
		out[id] = p
	}
	return out
}

type policyDirectory []provider.Model

func (d *policyDirectory) List(aliases map[string]string) []provider.Model {
	if aliases != nil {
		panic("policy list must use raw models")
	}
	return *d
}

func TestModelRecommendationsUseCurrentBoundsAndConfiguredOverride(t *testing.T) {
	st := policyStore{}
	dir := policyDirectory{}
	svc := NewModelPolicies(st, &dir)
	for _, p := range []string{"", provider.CodeArts} {
		if _, ok := svc.For(provider.Model{Provider: p, Context: 131072, MaxOutput: 8192}); ok {
			t.Fatal("inferred native capabilities")
		}
	}
	for _, bounds := range [][2]int{{8191, 8192}, {131072, 255}, {-1, -1}, {8192, 256}, {65536, 8192}, {9999999, 999999}} {
		m := provider.Model{ID: "model", Provider: provider.WorkBuddy, Context: bounds[0], MaxOutput: bounds[1]}
		p, ok := svc.For(m)
		if ok != (bounds[0] >= 8192 && bounds[1] >= 256) {
			t.Fatal("changed recommendation threshold", bounds, p, ok)
		}
		if ok && (p.Validate() != nil || p.ContextWindow > 2097152 || p.MaxOutputTokens > 8192 || p.SummaryEnabled || p.SummaryRetry) {
			t.Fatal("unsafe recommendation", p)
		}
	}
	// Existing configured values win even for absent/invalid catalog bounds.
	st["manual"] = ModelPolicy{ContextWindow: 123, Source: "existing value"}
	got, ok := svc.For(provider.Model{ID: "manual"})
	if !ok || got != st["manual"] {
		t.Fatal("configured policy no longer wins", got)
	}
	dir = append(dir, provider.Model{ID: "dynamic", Provider: provider.Trae, Context: 65536, MaxOutput: 4096})
	all := svc.All()
	if len(all) != 2 || all["dynamic"].MaxOutputTokens != 4096 || all["manual"] != st["manual"] {
		t.Fatal("lost configured or catalog policies", all)
	}
	delete(all, "manual")
	st["dynamic"] = DefaultModelPolicies()["deepseek-v4.1-flash"]
	if got := svc.All(); len(got) != 2 || got["dynamic"] != st["dynamic"] {
		t.Fatal("recommendation service cached or mutated policies", got)
	}
}

func TestCommonPolicyConservativeMergeAndOrderedShortCircuit(t *testing.T) {
	a := DefaultModelPolicies()["deepseek-v4.1-flash"]
	b := a
	b.ContextWindow = 32768
	b.MaxOutputTokens = 4096
	b.ReserveTokens = 20000
	b.TriggerTokens = 12000
	b.KeepRecentTokens = 4000
	b.SummaryMaxTokens = 4096
	a.SummaryFallbackModel = "native-fallback"
	policies := policyStore{"a": a, "b": b}
	visited := []string{}
	lookup := func(id string) (ModelPolicy, bool) { visited = append(visited, id); return policies.ModelPolicy(id) }
	combined, ok := CommonModelPolicy([]string{"a", "b"}, lookup)
	if !ok || combined.ContextWindow != 32768 || combined.MaxOutputTokens != 4096 || combined.ReserveTokens != 20000 || combined.TriggerTokens != 12000 || combined.KeepRecentTokens != 4000 || combined.SummaryEnabled || combined.SummaryRetry || combined.SummaryFallbackModel != "" || combined.SummaryMaxTokens != 4096 || combined.DisableSummaryThinking != a.DisableSummaryThinking || !reflect.DeepEqual(visited, []string{"a", "b"}) {
		t.Fatal("changed common policy", combined, visited)
	}
	visited = nil
	if p, ok := CommonModelPolicy([]string{"a", "missing", "b"}, lookup); ok || p != (ModelPolicy{}) || !reflect.DeepEqual(visited, []string{"a", "missing"}) {
		t.Fatal("did not stop at first missing policy", p, visited)
	}
	if _, ok := CommonModelPolicy(nil, func(string) (ModelPolicy, bool) { t.Fatal("empty list performed lookup"); return ModelPolicy{}, false }); ok {
		t.Fatal("empty list acquired policy")
	}
	policies["invalid"] = ModelPolicy{ContextWindow: 1}
	if p, ok := CommonModelPolicy([]string{"invalid"}, lookup); ok || p.ContextWindow != 1 || p.Source == "" {
		t.Fatal("invalid result projection changed", p, ok)
	}
}
