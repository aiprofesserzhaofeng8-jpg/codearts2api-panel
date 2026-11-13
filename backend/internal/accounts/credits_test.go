package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

type creditAdapter struct {
	provider.Adapter
	call func(context.Context) (json.RawMessage, error)
}

func (a creditAdapter) Credit(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return a.call(ctx)
}

func TestCreditFailureCacheAndInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		mode     string
		cached   bool
	}{
		{"native-credentials", provider.CodeArts, "resolve", true},
		{"native-fetch", provider.CodeArts, "fetch", true},
		{"external-credentials", provider.WorkBuddy, "resolve", false},
		{"external-fetch", provider.WorkBuddy, "fetch", true},
		{"external-json", provider.WorkBuddy, "json", false},
		{"external-null", provider.Trae, "null", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Credits{}
			a := &Account{ID: "a", Provider: tc.platform}
			calls := 0
			failure := errors.New("fixture failure")
			ops := CreditOperations{
				Native: func(ctx context.Context, _ *Account) (*CreditInfo, error) {
					calls++
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 15*time.Second {
						t.Error("query deadline lost")
					}
					return nil, failure
				},
				Provider: func(context.Context, *Account) (provider.Adapter, json.RawMessage, error) {
					calls++
					if tc.mode == "resolve" {
						return nil, nil, failure
					}
					return creditAdapter{call: func(context.Context) (json.RawMessage, error) {
						switch tc.mode {
						case "fetch":
							return nil, failure
						case "json":
							return json.RawMessage(`{`), nil
						default:
							return json.RawMessage(`null`), nil
						}
					}}, nil, nil
				},
			}
			for i := 0; i < 2; i++ {
				_, _ = c.For(t.Context(), a, false, ops)
			}
			want := 2
			if tc.cached {
				want = 1
			}
			if calls != want {
				t.Fatalf("calls %d want %d", calls, want)
			}
			if _, _, ok := c.Cached(a.ID); ok != tc.cached {
				t.Fatal("cache policy changed")
			}
			_, _ = c.For(t.Context(), a, true, ops)
			if calls != want+1 {
				t.Fatal("fresh did not bypass cache")
			}
			c.Invalidate(a.ID)
			if _, _, ok := c.Cached(a.ID); ok {
				t.Fatal("invalidation failed")
			}
		})
	}
	c := &Credits{}
	snap := &CreditInfo{Remaining: 42}
	c.Store("a", snap, nil)
	if got, _, ok := c.Cached("a"); !ok || got != snap {
		t.Fatal("immutable snapshot pointer changed")
	}
	c.creditCache["a"] = creditCacheEntry{info: snap, at: time.Now().Add(-61 * time.Second)}
	if _, _, ok := c.Cached("a"); ok {
		t.Fatal("expired cache returned")
	}
}

func TestCreditRowsBoundConcurrencyAndRetainOrder(t *testing.T) {
	c := &Credits{}
	var active, peak, calls atomic.Int32
	entered := make(chan struct{}, 4)
	finish := make(chan struct{})
	targets := make([]*Account, 9)
	for i := range targets {
		targets[i] = &Account{ID: fmt.Sprint(i)}
	}
	ops := CreditOperations{Native: func(ctx context.Context, a *Account) (*CreditInfo, error) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-finish
		if a.ID == "2" {
			return nil, errors.New("one account failed")
		}
		return &CreditInfo{Available: true}, nil
	}}
	done := make(chan []map[string]any, 1)
	go func() { done <- c.Rows(t.Context(), targets, true, ops) }()
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("batch did not fill four slots")
		}
	}
	if calls.Load() != 4 {
		t.Fatal("batch exceeded four concurrent queries")
	}
	close(finish)
	rows := <-done
	if calls.Load() != 9 || peak.Load() != 4 {
		t.Fatalf("calls=%d peak=%d", calls.Load(), peak.Load())
	}
	for i, row := range rows {
		if row["id"] != fmt.Sprint(i) || row["ok"] != (i != 2) {
			t.Fatalf("row %d: %#v", i, row)
		}
	}
}

func TestQuotaSnapshotNeverQueriesAndRetainsStaleness(t *testing.T) {
	c := &Credits{}
	if _, at, _, ok := c.Snapshot("missing"); ok || !at.IsZero() {
		t.Fatal("missing quota fabricated")
	}
	c.Store("fixture", &CreditInfo{Available: true, Remaining: 0}, nil)
	c.creditMu.Lock()
	e := c.creditCache["fixture"]
	e.at = time.Now().Add(-2 * time.Minute)
	c.creditCache["fixture"] = e
	c.creditMu.Unlock()
	info, at, stale, ok := c.Snapshot("fixture")
	if !ok || !stale || at.IsZero() || !info.Available || info.Remaining != 0 {
		t.Fatal(info, at, stale, ok)
	}
	if _, _, fresh := c.Cached("fixture"); fresh {
		t.Fatal("stale read became fresh")
	}
}

func TestCreditRowsCancelledBatchDoesNotQueryUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := &Credits{}
	rows := c.Rows(ctx, []*Account{{ID: "a"}, {ID: "b"}}, true, CreditOperations{Native: func(context.Context, *Account) (*CreditInfo, error) {
		t.Error("cancelled query reached upstream")
		return nil, nil
	}})
	if len(rows) != 2 || rows[0]["ok"] != false || rows[1]["ok"] != false {
		t.Fatal("missing cancelled result", rows)
	}
}
