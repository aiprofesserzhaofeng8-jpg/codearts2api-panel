package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/apierr"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/storage"
)

func runtimeFixture(t *testing.T, accts ...*accounts.Account) (*Runtime, *storage.Store, *registry.Registry) {
	t.Helper()
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accts {
		st.UpsertAccount(a)
	}
	directory := registry.New()
	return NewRuntime(st, directory), st, directory
}

func TestRefreshRetainsPartitionsAndPartialSuccess(t *testing.T) {
	r, st, directory := runtimeFixture(t,
		&accounts.Account{ID: "disabled", Provider: "trae", Scope: "cn"},
		&accounts.Account{ID: "first", Provider: "workbuddy", Scope: "cn", Enabled: true},
		&accounts.Account{ID: "second", Provider: "workbuddy", Scope: "cn", Enabled: true},
		&accounts.Account{ID: "third", Provider: "workbuddy", Scope: "cn", Enabled: true},
		&accounts.Account{ID: "global", Provider: "workbuddy", Scope: "global", Enabled: true},
	)
	directory.ReplaceProvider("workbuddy", "global", []provider.Model{{ID: "retained"}})
	var calls []string
	fail := errors.New("partition failed")
	ops := RefreshOperations{Native: func(context.Context, *accounts.Account) ([]provider.Model, error) {
		t.Fatal("provider-only refresh selected native credentials")
		return nil, nil
	}, Provider: func(_ context.Context, a *accounts.Account) ([]provider.Model, error) {
		calls = append(calls, a.ID)
		if a.ID != "second" {
			return nil, fail
		}
		return []provider.Model{{ID: "fresh"}}, nil
	}}
	if err := r.Refresh(context.Background(), ops); err != nil || !reflect.DeepEqual(calls, []string{"first", "second", "global"}) {
		t.Fatal("partial success or per-partition fallback changed", err, calls)
	}
	if _, ok := directory.ResolveRaw("workbuddy/global/retained"); !ok {
		t.Fatal("failed partition was erased")
	}
	if _, ok := directory.ResolveRaw("workbuddy/cn/fresh"); !ok || r.Error() != apierr.NoAccount.Error() || r.LoadedAt().IsZero() {
		t.Fatal("provider catalog or legacy native error projection changed")
	}
	errs := r.ProviderErrors()
	if len(errs) != 1 || errs["workbuddy/global"] != fail.Error() {
		t.Fatal(errs)
	}
	errs["workbuddy/global"] = "must not publish"
	if r.ProviderErrors()["workbuddy/global"] != fail.Error() {
		t.Fatal("refresh metadata was exposed for mutation")
	}
	// A successful empty provider list clears only that partition, as before.
	ops.Provider = func(context.Context, *accounts.Account) ([]provider.Model, error) { return nil, nil }
	if err := r.Refresh(context.Background(), ops); err != nil || len(r.ProviderErrors()) != 0 || r.Error() != "" {
		t.Fatal("successful refresh did not clear stale failures", err)
	}
	if _, ok := directory.ResolveRaw("workbuddy/global/retained"); ok {
		t.Fatal("successful empty provider list no longer clears the partition")
	}
	// Native empty success is different: retain the old list and report failure.
	st.UpsertAccount(&accounts.Account{ID: "native", Enabled: true})
	ops.Native = func(context.Context, *accounts.Account) ([]provider.Model, error) { return nil, nil }
	before := directory.List(nil)
	if err := r.Refresh(context.Background(), ops); err != nil || r.Error() != ErrEmptyModelList.Error() || !reflect.DeepEqual(before, directory.List(nil)) {
		t.Fatal("empty native catalog or partial-success semantics changed", err)
	}
}

func TestFailedRefreshTTLAndSerialization(t *testing.T) {
	r, _, _ := runtimeFixture(t, &accounts.Account{ID: "native", Enabled: true})
	var calls, active, peak atomic.Int32
	fail := errors.New("native failure")
	ops := RefreshOperations{Native: func(ctx context.Context, _ *accounts.Account) ([]provider.Model, error) {
		calls.Add(1)
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 30*time.Second {
			t.Error("stale refresh lost the original independent deadline")
		}
		return nil, fail
	}}
	r.RefreshIfStale(time.Minute, ops)
	r.RefreshIfStale(time.Minute, ops)
	if calls.Load() != 1 || r.Error() != fail.Error() {
		t.Fatal("failed refresh was immediately repeated")
	}
	entered, unblock := make(chan struct{}, 2), make(chan struct{})
	ops.Native = func(context.Context, *accounts.Account) ([]provider.Model, error) {
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- struct{}{}
		<-unblock
		active.Add(-1)
		return []provider.Model{{ID: "fresh"}}, nil
	}
	done := make(chan error, 2)
	go func() { done <- r.Refresh(context.Background(), ops) }()
	<-entered
	go func() { done <- r.Refresh(context.Background(), ops) }()
	close(unblock)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if peak.Load() != 1 || r.Error() != "" {
		t.Fatal("refresh lock or error recovery changed")
	}
}

func TestProbeConcurrencyFallbackOrderAndIndependentRefresh(t *testing.T) {
	r, _, _ := runtimeFixture(t, &accounts.Account{ID: "first", Enabled: true}, &accounts.Account{ID: "second", Enabled: true})
	run, err := r.BeginProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	var active, peak, calls atomic.Int32
	entered, unblock := make(chan struct{}, 4), make(chan struct{})
	ops := ProbeOperations{Native: func(ctx context.Context, a *accounts.Account, model string) error {
		calls.Add(1)
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > probeAttemptTimeout {
			t.Error("probe lost per-account attempt deadline")
		}
		if a.ID == "first" {
			return errors.New("first account failed")
		}
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		if n <= 4 {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		<-unblock
		active.Add(-1)
		return nil
	}}
	models := []string{" a ", "b", "c", "d", "e", "f", "g", "h", "a", " "}
	type outcome struct {
		result ProbeResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { result, err := run.Run(context.Background(), "", models, ops); done <- outcome{result, err} }()
	for range 4 {
		<-entered
	}
	if other, err := r.BeginProbe(); !errors.Is(err, ErrProbeBusy) {
		if other != nil {
			other.Close()
		}
		t.Error("parallel probe was admitted")
	}
	// Refresh must not acquire the probe lock or consume probe concurrency.
	if err := r.Refresh(context.Background(), RefreshOperations{Native: func(context.Context, *accounts.Account) ([]provider.Model, error) {
		return []provider.Model{{ID: "fresh"}}, nil
	}}); err != nil {
		t.Error(err)
	}
	close(unblock)
	got := <-done
	if got.err != nil || got.result.Total != 8 || got.result.Available != 8 || peak.Load() != 4 || calls.Load() != 16 {
		t.Fatal("probe cardinality, retry or concurrency changed", got, peak.Load(), calls.Load())
	}
	for i, row := range got.result.Results {
		if row["model"] != string(rune('a'+i)) || row["account"] != "second" || row["ok"] != true || row["error"] != "first account failed" {
			t.Fatal("probe ordering or historical row fields changed", row)
		}
	}
}

func TestProbeCancellationAndPlatformScope(t *testing.T) {
	r, st, directory := runtimeFixture(t, &accounts.Account{ID: "native", Enabled: true},
		&accounts.Account{ID: "cn", Provider: "workbuddy", Scope: "cn", Enabled: true},
		&accounts.Account{ID: "global", Provider: "workbuddy", Scope: "global", Enabled: true})
	directory.Replace([]provider.Model{{ID: "native"}})
	directory.ReplaceProvider("workbuddy", "cn", []provider.Model{{ID: "first"}})
	directory.ReplaceProvider("workbuddy", "global", []provider.Model{{ID: "first"}})
	st.UpdateSettings(func(s *storage.Settings) { s.ModelAliases["duplicate"] = "native" })
	run, err := r.BeginProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	ops := ProbeOperations{Native: func(context.Context, *accounts.Account, string) error { return nil },
		Provider: func(ctx context.Context, a *accounts.Account, m provider.Model, body []byte) ([]byte, error) {
			if a.Scope != m.Scope || a.ProviderID() != m.Provider {
				t.Error("probe crossed platform/scope")
			}
			var request struct {
				Model     string `json:"model"`
				MaxTokens int    `json:"max_tokens"`
			}
			if json.Unmarshal(body, &request) != nil || request.Model != m.ID || request.MaxTokens != 64 {
				t.Error("provider probe payload changed")
			}
			return nil, nil
		}}
	result, err := run.Run(context.Background(), "", nil, ops)
	if err != nil || result.Total != 3 || result.Available != 3 {
		t.Fatal("all-model probe included aliases or lost a partition", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ops.Native = func(context.Context, *accounts.Account, string) error {
		t.Error("canceled native probe made a paid attempt")
		return nil
	}
	result, err = run.Run(ctx, "native", []string{"native"}, ops)
	if err != nil || result.Available != 0 || result.Results[0]["error"] != "测活已取消" {
		t.Fatal("native cancellation contract changed", result, err)
	}
	tooMany := make([]string, ProbeMaxModels+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprint(i)
	}
	if _, err := run.Run(context.Background(), "native", tooMany, ops); err == nil {
		t.Fatal("oversized paid probe was admitted")
	}
}
