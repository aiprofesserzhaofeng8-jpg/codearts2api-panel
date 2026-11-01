package codearts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestManagerRuntimeZeroValueAndLegacyPersistence(t *testing.T) {
	var m Manager
	original := &Session{AccessKey: "fixture-old", SecretKey: "fixture", ExpiresAt: time.Now().Add(-time.Hour)}
	updated := &Session{AccessKey: "fixture-new", SecretKey: "fixture", ExpiresAt: time.Now().Add(time.Hour)}
	acct := &Account{ID: "fixture", Session: original}
	saves, logs := 0, 0
	m.Persist = func(*Account) error { saves++; return errors.New("fixture persistence failure") }
	m.Logf = func(string, ...any) { logs++ }
	m.Remember(acct.ID, updated)
	for range 2 {
		got, err := m.Session(t.Context(), acct, ResolveOptions{})
		if err != nil || got != updated {
			t.Fatal("legacy persistence failure lost current session", err)
		}
	}
	if saves != 1 || logs != 1 {
		t.Fatal("unchanged generation persisted again")
	}
	m.Invalidate(acct.ID)
	if m.Cached(acct.ID, time.Hour) != nil {
		t.Fatal("invalidation left live cache")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Session(ctx, acct, ResolveOptions{}); !errors.Is(err, ctx.Err()) {
		t.Fatal("cancelled resolution ignored context", err)
	}
}

func TestManagerRuntimeInvalidationSharesRefreshLock(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	endpoint := startFakeTokenServer(t, func(string) { close(entered); <-release })
	m := managerForTest(endpoint)
	acct := &Account{ID: "refreshing", Session: refreshableSession(t, "fixture")}
	refreshed := make(chan error, 1)
	go func() { _, err := m.Session(t.Context(), acct, ResolveOptions{AllowRefresh: true}); refreshed <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh not entered")
	}
	invalidating, invalidated := make(chan struct{}), make(chan struct{})
	go func() { close(invalidating); m.Invalidate(acct.ID); close(invalidated) }()
	<-invalidating
	select {
	case <-invalidated:
		t.Fatal("invalidation bypassed active refresh")
	case <-time.After(20 * time.Millisecond):
	}
	// A different account still resolves while this refresh and invalidation wait.
	other, _ := ManualAKSKAccount("fixture", "fixture-permanent-ak", "fixture-permanent-sk")
	otherDone := make(chan error, 1)
	go func() { _, err := m.Session(t.Context(), other, ResolveOptions{}); otherDone <- err }()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("account locks became global")
	}
	unblock()
	select {
	case err := <-refreshed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh stuck")
	}
	select {
	case <-invalidated:
	case <-time.After(5 * time.Second):
		t.Fatal("invalidation stuck")
	}
	if m.Cached(acct.ID, time.Hour) != nil || StoredSession(acct).AccessKey != "new-ak-fixture" {
		t.Fatal("invalidation lost race with publication")
	}
}
