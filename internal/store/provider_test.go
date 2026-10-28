package store

import (
	"codearts2api/internal/codearts"
	"encoding/json"
	"testing"
)

func TestProviderIsolationAndDeduplication(t *testing.T) {
	s := testPool(t, 1)
	for _, scope := range []string{"cn", "global"} {
		a := &codearts.Account{ID: scope, Provider: "workbuddy", Scope: scope, ExternalID: "same-user", Enabled: true, Credentials: json.RawMessage(`{"access_token":"fixture"}`)}
		if _, err := s.UpsertAccountAndSave(a, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.Accounts()) != 3 {
		t.Fatal("provider identity collision")
	}
	for _, model := range []string{"workbuddy/cn/shared", "workbuddy/global/shared", "plain-codearts-model"} {
		a, release := s.AcquireAccountForModel("", model, nil)
		if a == nil {
			t.Fatal("no eligible account")
		}
		release()
		if model == "plain-codearts-model" && a.ProviderID() != "codearts" {
			t.Fatal("foreign credentials selected for legacy model")
		}
		if model == "workbuddy/cn/shared" && (a.ProviderID() != "workbuddy" || a.Scope != "cn") {
			t.Fatal("wrong realm selected")
		}
	}
	loaded, err := Open(s.Path())
	if err != nil || len(loaded.Accounts()) != 3 {
		t.Fatal("multi-provider state failed to reload")
	}
}
func TestProviderCredentialsSnapshotsAndCAS(t *testing.T) {
	s := testPool(t, 0)
	a := &codearts.Account{ID: "wb", Provider: "workbuddy", Scope: "cn", ExternalID: "u", Credentials: json.RawMessage(`{"token":"old"}`)}
	s.UpsertAccount(a)
	snap := s.Account("wb")
	snap.Credentials[0] = 'x'
	if s.Account("wb").Credentials[0] != '{' {
		t.Fatal("credential snapshot mutated state")
	}
	if err := s.CommitProviderCredentials("wb", a.Credentials, json.RawMessage(`{"token":"new"}`)); err != nil {
		t.Fatal(err)
	}
	if s.CommitProviderCredentials("wb", a.Credentials, json.RawMessage(`{"token":"stale"}`)) == nil {
		t.Fatal("stale refresh overwrote current token")
	}
}
func TestProviderUsageFilter(t *testing.T) {
	s := testPool(t, 0)
	s.Record(LogEntry{Provider: "workbuddy", Model: "workbuddy/cn/m", Status: 200, Usage: TokenUsage{Known: true, InputTokens: 20, OutputTokens: 5}}, 0)
	s.Record(LogEntry{Model: "m", Status: 200, Usage: TokenUsage{Known: true, InputTokens: 10}}, 0)
	a := s.QueryUsage(UsageQuery{Provider: "workbuddy"})
	b := s.QueryUsage(UsageQuery{Provider: "codearts"})
	if a.Total != 1 || a.Stats.InputTokens != 20 || b.Total != 1 || b.Stats.InputTokens != 10 {
		t.Fatal("provider totals crossed")
	}
}
