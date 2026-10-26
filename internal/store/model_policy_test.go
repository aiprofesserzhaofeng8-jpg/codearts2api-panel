package store

import (
	"path/filepath"
	"testing"
)

func TestModelPolicyValidationAndPersistence(t *testing.T) {
	p := DefaultModelPolicies()["deepseek-v4.1-flash"]
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ModelPolicy){func(p *ModelPolicy) { p.KeepRecentTokens = p.TriggerTokens }, func(p *ModelPolicy) { p.ReserveTokens = 100 }, func(p *ModelPolicy) { p.TriggerTokens = p.ContextWindow }, func(p *ModelPolicy) { p.SummaryMaxTokens = p.MaxOutputTokens + 1 }} {
		bad := p
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("incoherent policy accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p.SummaryRetry = false
	p.KeepRecentTokens = 8192
	if err := s.SetModelPolicyAndSave("deepseek-v4.1-flash", p); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := loaded.ModelPolicy("deepseek-v4.1-flash")
	if !ok || got != p {
		t.Fatal("policy not persisted")
	}
	policies := loaded.ModelPolicies()
	delete(policies, "deepseek-v4.1-flash")
	if _, ok := loaded.ModelPolicy("deepseek-v4.1-flash"); !ok {
		t.Fatal("snapshot modified internal policy")
	}
	if _, ok := loaded.ModelPolicy("unknown"); ok {
		t.Fatal("unknown model advertised as verified")
	}
}
