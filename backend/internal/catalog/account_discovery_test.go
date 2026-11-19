package catalog

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

func TestAccountDiscoveryIsolatedFromPartition(t *testing.T) {
	r, _, directory := runtimeFixture(t, &accounts.Account{ID: "a", Provider: "qoder", Scope: "cn", Enabled: true}, &accounts.Account{ID: "b", Provider: "qoder", Scope: "cn", Enabled: true}, &accounts.Account{ID: "off", Provider: "qoder"}, &accounts.Account{ID: "other", Provider: "workbuddy", Enabled: true})
	directory.ReplaceProvider("qoder", "cn", []provider.Model{{ID: "global"}})
	before := directory.List(nil)
	answers := map[string][]provider.Model{"a": {{ID: "first"}, {ID: "stable", Name: "Old", Context: 8192}}, "b": {{ID: "b-only"}}}
	var calls []string
	fetch := func(_ context.Context, a *accounts.Account) ([]provider.Model, error) {
		calls = append(calls, a.ID)
		rows, ok := answers[a.ID]
		if !ok {
			return nil, errors.New("unavailable")
		}
		return rows, nil
	}
	discover := func(id string) *AccountModelList {
		t.Helper()
		v, err := r.DiscoverAccountModels(context.Background(), id, fetch)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	a, b := discover("a"), discover("b")
	if a.Models[1].Context != 8192 || b.Models[0].ID != "b-only" || a.ObservedAt.IsZero() {
		t.Fatal(a, b)
	}
	if _, err := a.Diff(b); err == nil {
		t.Fatal("mixed accounts")
	}
	answers["a"] = []provider.Model{{ID: "stable", Name: "New"}, {ID: "added"}}
	d, err := discover("a").Diff(a)
	if err != nil || !reflect.DeepEqual(d, AccountModelDiff{Added: []string{"added"}, Missing: []string{"first"}, Retained: []string{"stable"}}) {
		t.Fatal(d, err)
	}
	delete(answers, "b")
	if list, err := r.DiscoverAccountModels(context.Background(), "b", fetch); list != nil || err == nil {
		t.Fatal("failed discovery produced a result")
	}
	for _, id := range []string{"missing", "off"} {
		if _, err := r.DiscoverAccountModels(context.Background(), id, fetch); err == nil {
			t.Fatal(id)
		}
	}
	if !reflect.DeepEqual(calls, []string{"a", "b", "a", "b"}) || !reflect.DeepEqual(before, directory.List(nil)) {
		t.Fatal("unexpected account or directory mutation", calls)
	}
}
