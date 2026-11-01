package codearts

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginCommitIsSingleAndRetryableAfterSaveFailure(t *testing.T) {
	f := &LoginFlow{Status: LoginConfirmed, Finished: time.Now(), Session: &Session{AccessKey: "a", SecretKey: "s", ExpiresAt: time.Now().Add(time.Hour)}}
	if _, err := f.Commit(func(*Session) (string, error) { return "", errors.New("disk unavailable") }); err == nil || f.SavedAccountID() != "" {
		t.Fatal("failed commit marked as saved")
	}
	var writes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := f.Commit(func(*Session) (string, error) { writes.Add(1); return "account-id", nil })
			if err != nil || id != "account-id" {
				t.Error("commit failed")
			}
		}()
	}
	wg.Wait()
	if writes.Load() != 1 {
		t.Fatal("duplicate commits wrote multiple times")
	}
}

func TestCancelConfirmedLoginCannotBeCommitted(t *testing.T) {
	m := NewLoginManager(NewClient())
	f := &LoginFlow{ID: "test", closed: true, Status: LoginConfirmed, Finished: time.Now(), Session: &Session{AccessKey: "a", SecretKey: "s", ExpiresAt: time.Now().Add(time.Hour)}}
	m.flows[f.ID] = f
	if !m.Cancel(f.ID) {
		t.Fatal("confirmed staged flow could not be canceled")
	}
	if _, err := f.Commit(func(*Session) (string, error) { t.Fatal("canceled credential saved"); return "", nil }); err == nil {
		t.Fatal("canceled flow accepted")
	}
}

func TestCompletedLoginAcceptsSameCallbackWithoutExchange(t *testing.T) {
	for _, kind := range []string{"oauth", "ticket"} {
		t.Run(kind, func(t *testing.T) {
			f := &LoginFlow{Port: 12345, closed: true, Status: LoginConfirmed, Finished: time.Now(), Session: &Session{AccessKey: "a", SecretKey: "s", ExpiresAt: time.Now().Add(time.Hour)}}
			raw := "http://127.0.0.1:12345/oauth/callback?"
			if kind == "oauth" {
				f.code = "oauth-code"
				raw += "code=oauth-code"
			} else {
				f.secret = "ticket-secret"
				raw += "secret=ticket-secret"
			}
			// A nil client proves accepting the replay does not start a second exchange.
			for i := 0; i < 3; i++ {
				if _, err := f.SubmitCallbackURL(raw); err != nil {
					t.Fatalf("duplicate callback should return the existing preview: %v", err)
				}
			}
			if f.Status != LoginConfirmed || f.saved {
				t.Fatal("replay changed state or saved without confirmation")
			}
			if err := f.SubmitCode("different-code"); err == nil {
				t.Fatal("different authorization accepted into completed flow")
			}
		})
	}
}
func TestTerminalLoginSubmissionPreservesReason(t *testing.T) {
	for _, status := range []LoginStatus{LoginFailed, LoginExpired, LoginCanceled} {
		f := &LoginFlow{closed: true, Status: status, Message: "specific-reason", code: "oauth-code"}
		err := f.SubmitCode("oauth-code")
		if err == nil || !strings.Contains(err.Error(), "specific-reason") {
			t.Fatalf("terminal reason lost: %v", err)
		}
	}
}
