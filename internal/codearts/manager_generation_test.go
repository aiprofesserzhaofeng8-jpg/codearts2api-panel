package codearts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRefreshCannotOverwriteConcurrentLogin(t *testing.T) {
	entered, proceed := make(chan struct{}), make(chan struct{})
	endpoint := startFakeTokenServer(t, func(string) { close(entered); <-proceed })
	m := managerForTest(endpoint)
	var mu sync.Mutex
	current := &Account{ID: "a", Session: refreshableSession(t, "original")}
	m.Latest = func(id string) *Account { mu.Lock(); defer mu.Unlock(); copy := *current; return &copy }
	m.Commit = func(id string, expected, updated *Session) error {
		mu.Lock()
		defer mu.Unlock()
		if current.Session != expected {
			return ErrSessionChanged
		}
		current.Session = updated
		return nil
	}
	old := *current
	done := make(chan error, 1)
	go func() {
		_, err := m.Session(context.Background(), &old, ResolveOptions{AllowRefresh: true})
		done <- err
	}()
	<-entered
	fresh := &Session{AccessKey: "manual-new", SecretKey: "secret", ExpiresAt: time.Now().Add(time.Hour)}
	mu.Lock()
	current.Session = fresh
	mu.Unlock()
	close(proceed)
	if err := <-done; !errors.Is(err, ErrSessionChanged) {
		t.Fatal("stale refresh was accepted")
	}
	stale := old
	got, err := m.Session(context.Background(), &stale, ResolveOptions{AllowRefresh: true, LiveTTL: time.Nanosecond})
	if err != nil || got != fresh {
		t.Fatal("new login not preferred over stale snapshot/cache")
	}
}
