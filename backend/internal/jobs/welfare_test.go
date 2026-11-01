package jobs

import (
	"reflect"
	"testing"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

type welfareStore struct {
	accounts []*accounts.Account
	next     *accounts.Account
	calls    []string
}

func (s *welfareStore) Account(id string) *accounts.Account {
	s.calls = append(s.calls, "account:"+id)
	for _, a := range s.accounts {
		if a.ID == id {
			return a
		}
	}
	return nil
}
func (s *welfareStore) NextAccount() *accounts.Account {
	s.calls = append(s.calls, "next")
	return s.next
}
func (s *welfareStore) Accounts() []*accounts.Account {
	s.calls = append(s.calls, "accounts")
	return s.accounts
}

type welfareRewardAdapter struct{ rewardAdapter }

func (welfareRewardAdapter) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "optional"}
}

func TestWelfareSelectionAndLiveCapabilities(t *testing.T) {
	disabled := &accounts.Account{ID: "disabled", Enabled: false}
	unsupported := &accounts.Account{ID: "unsupported", Provider: "unsupported", Enabled: true}
	optional := &accounts.Account{ID: "optional", Provider: "optional", Enabled: true}
	fallback := &accounts.Account{ID: "fallback", Enabled: true}
	scheduled := &accounts.Account{ID: "scheduled", Enabled: true}
	st := &welfareStore{accounts: []*accounts.Account{disabled, unsupported, optional, fallback}, next: scheduled}
	w := NewWelfare(st)
	registry := provider.NewRegistry()
	cases := []struct {
		name, id   string
		invite     bool
		next, want *accounts.Account
		calls      []string
	}{
		{"explicit-disabled", "disabled", false, scheduled, disabled, []string{"account:disabled"}},
		{"missing-uses-scheduler", "missing", false, scheduled, scheduled, []string{"account:missing", "next"}},
		{"untrimmed", " disabled ", false, scheduled, scheduled, []string{"account: disabled ", "next"}},
		{"list-supported-fallback", "", false, nil, fallback, []string{"next", "accounts"}},
		{"invite-no-daily-fallback", "", true, nil, nil, []string{"next"}},
		{"invite-disabled", "disabled", true, nil, disabled, []string{"account:disabled"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st.calls = nil
			st.next = tc.next
			var got *accounts.Account
			if tc.invite {
				got = w.ForInvite(tc.id)
			} else {
				got = w.ForList(tc.id, registry)
			}
			if got != tc.want || !reflect.DeepEqual(st.calls, tc.calls) {
				t.Fatalf("selection=%v calls=%v", got, st.calls)
			}
		})
	}
	if err := registry.Register(welfareRewardAdapter{}); err != nil {
		t.Fatal(err)
	}
	st.next = nil
	st.calls = nil
	if w.ForList("", registry) != optional {
		t.Fatal("registry capabilities cached or ignored")
	}
	options := w.AccountOptions(registry)
	if len(options) != 3 || options[0]["id"] != "disabled" || options[0]["enabled"] != false || options[1]["id"] != "optional" || options[2]["id"] != "fallback" {
		t.Fatal(options)
	}
	st.accounts = nil
	if w.ForList("", registry) != nil || w.AccountOptions(registry) == nil {
		t.Fatal("empty list semantics changed")
	}
}
