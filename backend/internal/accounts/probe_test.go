package accounts

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

type probeStore struct {
	list    []*Account
	events  []string
	saveErr error
	current context.Context
}

func (s *probeStore) Accounts() []*Account { s.events = append(s.events, "accounts"); return s.list }
func (s *probeStore) Account(id string) *Account {
	s.events = append(s.events, "account:"+id)
	for _, a := range s.list {
		if a.ID == id {
			return a
		}
	}
	return nil
}
func (s *probeStore) MarkAccountResult(id string, ok bool, note string) {
	if s.current != nil && s.current.Err() != context.Canceled {
		panic("result marked before child cancellation")
	}
	s.events = append(s.events, "mark:"+id+":"+note)
}
func (s *probeStore) Save() error { s.events = append(s.events, "save"); return s.saveErr }

type probeDirectory struct {
	t     *testing.T
	store *probeStore
}

func (d probeDirectory) ReplaceProvider(p, scope string, list []provider.Model) {
	if d.store.current.Err() != nil {
		d.t.Fatal("catalog replaced after cancellation")
	}
	if p != provider.Trae || scope != "global" || len(list) != 0 {
		d.t.Fatal("wrong catalog partition", p, scope, list)
	}
	d.store.events = append(d.store.events, "replace:"+p+":"+scope)
}

func TestProbesPreserveSequenceDeadlinesPartialResultsAndSaveFailure(t *testing.T) {
	st := &probeStore{list: []*Account{
		{ID: "bad", Provider: provider.WorkBuddy, Scope: "cn", Name: "disabled", Enabled: false},
		{ID: "good", Provider: provider.Trae, Scope: "global", Paused: true},
		{ID: "native", Name: "native"},
	}, saveErr: errors.New("disk unavailable")}
	logged := 0
	info := &ProbeCredentialInfo{ExpiresAt: -62135596800, AccessKey: "masked"}
	checkDeadline := func(ctx context.Context, want time.Duration) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > want || time.Until(deadline) < want-time.Second {
			t.Fatal("changed account deadline", deadline)
		}
		st.current = ctx
	}
	got := NewProbes(st, probeDirectory{t, st}, func(string, ...any) { logged++ }).Run(t.Context(), "", ProbeOperations{
		Invalidate: func(id string) { st.events = append(st.events, "invalidate:"+id) },
		Provider: func(ctx context.Context, a *Account) ([]provider.Model, error) {
			checkDeadline(ctx, 30*time.Second)
			st.events = append(st.events, "provider:"+a.ID)
			if a.ID == "bad" {
				return []provider.Model{{ID: "not-published"}}, errors.New("catalog rejected")
			}
			return nil, nil
		},
		Native: func(ctx context.Context, a *Account) (*ProbeCredentialInfo, error) {
			checkDeadline(ctx, time.Minute)
			st.events = append(st.events, "native:"+a.ID)
			return info, errors.New("model rejected")
		},
	})
	wantEvents := []string{"accounts", "provider:bad", "mark:bad:catalog rejected", "provider:good", "replace:trae:global", "mark:good:", "invalidate:native", "native:native", "mark:native:model rejected", "save"}
	if !reflect.DeepEqual(st.events, wantEvents) || logged != 1 {
		t.Fatal("changed operation order", st.events, logged)
	}
	if len(got) != 3 || got[0].OK || !got[1].OK || got[2].OK || got[2].Credentials != info || got[0].Credentials != nil || got[0].Name != "disabled" {
		t.Fatal("lost partial probe results", got)
	}
}

func TestProbesExactTargetSelectionAndEmptySave(t *testing.T) {
	for _, id := range []string{"unknown", " present ", "present"} {
		t.Run(id, func(t *testing.T) {
			st := &probeStore{list: []*Account{{ID: "present", Provider: provider.WorkBuddy}}}
			calls := 0
			got := NewProbes(st, probeDirectory{t, st}, nil).Run(t.Context(), id, ProbeOperations{Provider: func(ctx context.Context, a *Account) ([]provider.Model, error) {
				calls++
				st.current = ctx
				return nil, errors.New("fixture")
			}})
			want := 0
			if id == "present" {
				want = 1
			}
			if got == nil || len(got) != want || calls != want || st.events[0] != "accounts" || st.events[1] != "account:"+id || st.events[len(st.events)-1] != "save" {
				t.Fatal("changed empty/explicit target behavior", got, st.events)
			}
		})
	}
}

func TestProbesParentCancellationRetainsPerAccountResults(t *testing.T) {
	st := &probeStore{list: []*Account{{ID: "a", Provider: provider.WorkBuddy}, {ID: "b", Provider: provider.Trae}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := NewProbes(st, probeDirectory{t, st}, nil).Run(ctx, "", ProbeOperations{Provider: func(ctx context.Context, a *Account) ([]provider.Model, error) { return nil, ctx.Err() }})
	if len(got) != 2 || got[0].Error != context.Canceled.Error() || got[1].Error != context.Canceled.Error() || st.events[len(st.events)-1] != "save" {
		t.Fatal("changed canceled batch behavior", got, st.events)
	}
}
