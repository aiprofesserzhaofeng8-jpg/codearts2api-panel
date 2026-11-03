package access

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type installationCommitFunc func(string, string) error

func (f installationCommitFunc) InstallInitialAdmin(email, hash string) error { return f(email, hash) }

func TestInstallationValidatesBeforeHashOrCommit(t *testing.T) {
	var calls int
	i := NewInstallation(installationCommitFunc(func(string, string) error { calls++; return nil }))
	for _, tc := range []struct{ email, password, confirmation string }{
		{"wrong", "long-password-here", "long-password-here"},
		{"", "long-password-here", "long-password-here"},
		{"owner@example.com", "short", "short"},
		{"owner@example.com", "long-password-here", "different-password"},
	} {
		if err := i.Install(context.Background(), tc.email, tc.password, tc.confirmation); err == nil {
			t.Fatal("invalid installation accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := i.Install(ctx, "owner@example.com", "long-password-here", "long-password-here"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 0 || !i.Pending() {
		t.Fatal("validation mutated installation")
	}
}

func TestInstallationFailureRetryAndSingleCommit(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	failure := errors.New("disk unavailable")
	i := NewInstallation(installationCommitFunc(func(email, hash string) error {
		if !VerifyAdminPassword(hash, "long-password-here") {
			t.Error("not a usable hash")
		}
		if calls.Add(1) == 1 {
			return failure
		}
		close(entered)
		<-release
		return nil
	}))
	if err := i.Install(context.Background(), "owner@example.com", "long-password-here", "long-password-here"); !errors.Is(err, failure) || !i.Pending() {
		t.Fatal("failed commit cannot retry", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- i.Install(context.Background(), "owner@example.com", "long-password-here", "long-password-here")
	}()
	<-entered
	if err := i.Install(context.Background(), "owner@example.com", "long-password-here", "long-password-here"); !errors.Is(err, ErrInstallationBusy) {
		t.Fatal("parallel hash/commit allowed", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if i.Pending() {
		t.Fatal("installation still open")
	}
	if err := i.Install(context.Background(), "owner@example.com", "replacement-password", "replacement-password"); !errors.Is(err, ErrInstallationClosed) || calls.Load() != 2 {
		t.Fatal("reinitialization accepted", err)
	}
}
