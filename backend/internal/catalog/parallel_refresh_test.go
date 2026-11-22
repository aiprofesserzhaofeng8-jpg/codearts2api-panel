package catalog

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

func TestRefreshQueueCancellationAndJoin(t *testing.T) {
	var list []*accounts.Account
	for i := range 6 {
		list = append(list, &accounts.Account{ID: fmt.Sprint(i), Provider: "fixture", Scope: fmt.Sprint(i), Enabled: true})
	}
	r, _, _ := runtimeFixture(t, list...)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r.BindRefreshContext(ctx)
	entered := make(chan struct{}, 6)
	var calls atomic.Int32
	ops := RefreshOperations{Provider: func(ctx context.Context, _ *accounts.Account) ([]provider.Model, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	round := r.beginRound(t.Context(), 0, ops)
	for range 4 {
		<-entered
	}
	for range 20 {
		if r.beginRound(t.Context(), time.Minute, ops) != round {
			t.Fatal("duplicate round")
		}
	}
	waiter, stop := context.WithCancel(t.Context())
	stop()
	if r.JoinRefresh(waiter, ops) != context.Canceled {
		t.Fatal("canceled waiter blocked")
	}
	cancel()
	<-round.done
	counts := map[string]int{}
	for _, s := range r.RefreshReport().Partitions {
		counts[s.Status]++
	}
	if calls.Load() != 4 || counts["not_started"] != 2 || counts["failed"] != 4 {
		t.Fatal(calls.Load(), counts)
	}
}

func TestProbeDuringReferenceFetchKeepsNewerCatalog(t *testing.T) {
	a := &accounts.Account{ID: "a", Provider: "trae", Scope: "cn", Enabled: true}
	r, st, d := runtimeFixture(t, a)
	r.loadReferences = func(context.Context) ([]byte, error) {
		if _, ok := d.ResolveRaw("trae/cn/initial"); !ok {
			t.Fatal("model not available before optional references")
		}
		if err := r.CommitProbePartition(a, []provider.Model{{ID: "probe"}}); err != nil {
			t.Fatal(err)
		}
		return []byte(`{"openai":{"models":{"glm-5.3":{"id":"glm-5.3","limit":{"context":8192}}}}}`), nil
	}
	err := r.Refresh(t.Context(), RefreshOperations{Provider: func(context.Context, *accounts.Account) ([]provider.Model, error) {
		return []provider.Model{{ID: "initial"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.ResolveRaw("trae/cn/probe"); !ok {
		t.Fatal("metadata overwrote newer probe")
	}
	st.RestoreCatalogPartitions(func(_, _ string, models []provider.Model, _ time.Time) {
		if len(models) != 1 || models[0].ID != "probe" {
			t.Fatal(models)
		}
	})
}
