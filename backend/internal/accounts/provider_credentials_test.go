package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
)

type credentialFixture struct {
	Token   string
	ID      string
	Scope   string
	Expires int64
}

func rawCredential(token, id, scope string, valid bool) json.RawMessage {
	expires := time.Now().Add(-time.Hour)
	if valid {
		expires = time.Now().Add(time.Hour)
	}
	raw, _ := json.Marshal(credentialFixture{token, id, scope, expires.Unix()})
	return raw
}

func sameCredentialJSON(a, b []byte) bool {
	var left, right bytes.Buffer
	return json.Compact(&left, a) == nil && json.Compact(&right, b) == nil && bytes.Equal(left.Bytes(), right.Bytes())
}

type credentialAdapter struct {
	normalizes atomic.Int32
	refreshes  atomic.Int32
	refresh    func(context.Context, json.RawMessage) (json.RawMessage, error)
}

func (a *credentialAdapter) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	a.normalizes.Add(1)
	var c credentialFixture
	err := json.Unmarshal(raw, &c)
	return raw, provider.Identity{ID: c.ID, Scope: c.Scope, ExpiresAt: c.Expires}, err
}

func (a *credentialAdapter) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a.refreshes.Add(1)
	return a.refresh(ctx, raw)
}

func credentialStore(t *testing.T, valid bool) (*store.Store, *accounts.ProviderCredentials) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state-dir", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateSettings(func(s *store.Settings) { s.AutoRefresh = true })
	st.UpsertAccount(&accounts.Account{ID: "a", Provider: provider.WorkBuddy, Scope: "cn", ExternalID: "user", Credentials: rawCredential("original", "user", "cn", valid)})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	return st, accounts.NewProviderCredentials(st, func() bool { return st.Settings().AutoRefresh })
}

func TestProviderCredentialsNormalPathAndHotRefreshSetting(t *testing.T) {
	st, service := credentialStore(t, true)
	a := &credentialAdapter{refresh: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return rawCredential("rotated", "user", "cn", true), nil
	}}
	before, _ := os.ReadFile(st.Path())
	raw, err := service.Resolve(t.Context(), "a", a)
	if err != nil || !bytes.Equal(raw, st.Account("a").Credentials) || a.normalizes.Load() != 1 || a.refreshes.Load() != 0 {
		t.Fatal("valid credentials must normalize once without refresh", err)
	}
	after, _ := os.ReadFile(st.Path())
	if !bytes.Equal(before, after) {
		t.Fatal("normal resolution wrote state")
	}
	acct := st.Account("a")
	acct.Credentials = rawCredential("expired", "user", "cn", false)
	st.UpsertAccount(acct)
	st.UpdateSettings(func(s *store.Settings) { s.AutoRefresh = false })
	_, err = service.Resolve(t.Context(), "a", a)
	var fault *provider.Error
	if !errors.As(err, &fault) || fault.Status != 401 || !fault.AccountFault || a.refreshes.Load() != 0 {
		t.Fatal("disabled refresh changed behavior", err)
	}
	st.UpdateSettings(func(s *store.Settings) { s.AutoRefresh = true })
	if _, err = service.Resolve(t.Context(), "a", a); err != nil || a.refreshes.Load() != 1 {
		t.Fatal("current refresh setting was not observed", err)
	}
	reloaded, err := store.Open(st.Path())
	if err != nil || !sameCredentialJSON(reloaded.Account("a").Credentials, st.Account("a").Credentials) {
		t.Fatal("refresh was not persisted", err)
	}
}

func TestProviderCredentialsSerializeRefreshAllowOtherAccountsAndCancelWaiter(t *testing.T) {
	st, service := credentialStore(t, false)
	st.UpsertAccount(&accounts.Account{ID: "b", Provider: provider.Trae, Scope: "global", ExternalID: "other", Credentials: rawCredential("other", "other", "global", true)})
	entered, proceed := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(proceed) }) })
	a := &credentialAdapter{refresh: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		close(entered)
		select {
		case <-proceed:
			return rawCredential("rotated", "user", "cn", true), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	results := make(chan error, 24)
	go func() { _, err := service.Resolve(t.Context(), "a", a); results <- err }()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.Resolve(ctx, "a", a); !errors.Is(err, context.Canceled) {
		t.Fatal("waiting for a busy account ignored cancellation", err)
	}
	otherCtx, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if _, err := service.Resolve(otherCtx, "b", a); err != nil {
		t.Fatal("one account refresh blocked another account", err)
	}
	for range 23 {
		go func() { _, err := service.Resolve(t.Context(), "a", a); results <- err }()
	}
	unblock.Do(func() { close(proceed) })
	for range 24 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if a.refreshes.Load() != 1 {
		t.Fatal("queued requests reused a rotated refresh token")
	}
}

func TestProviderCredentialsRejectStaleRefreshAfterLoginOrDeletion(t *testing.T) {
	for _, action := range []string{"login", "delete"} {
		t.Run(action, func(t *testing.T) {
			st, service := credentialStore(t, false)
			entered, proceed := make(chan struct{}), make(chan struct{})
			a := &credentialAdapter{refresh: func(context.Context, json.RawMessage) (json.RawMessage, error) {
				close(entered)
				<-proceed
				return rawCredential("stale-refresh", "user", "cn", true), nil
			}}
			done := make(chan error, 1)
			go func() { _, err := service.Resolve(t.Context(), "a", a); done <- err }()
			<-entered
			fresh := rawCredential("new-login", "user", "cn", true)
			if action == "login" {
				acct := st.Account("a")
				acct.Credentials = fresh
				if _, err := st.UpsertAccountAndSave(acct, true); err != nil {
					close(proceed)
					t.Fatal(err)
				}
			} else if _, err := st.RemoveAccountAndSave("a"); err != nil {
				close(proceed)
				t.Fatal(err)
			}
			close(proceed)
			if err := <-done; !errors.Is(err, accounts.ErrCredentialChanged) {
				t.Fatal("stale refresh was committed", err)
			}
			raw, err := service.Resolve(t.Context(), "a", a)
			if action == "login" && (err != nil || !bytes.Equal(raw, fresh)) {
				t.Fatal("resolution did not use the newer login", err)
			}
			if action == "delete" && (!errors.Is(err, accounts.ErrCredentialChanged) || st.Account("a") != nil) {
				t.Fatal("deleted account was resurrected", err)
			}
			if a.refreshes.Load() != 1 {
				t.Fatal("old refresh token was used twice")
			}
		})
	}
}

func TestProviderCredentialsPersistenceFailureRetainsRotatedGeneration(t *testing.T) {
	st, service := credentialStore(t, false)
	dir := filepath.Dir(st.Path())
	if err := os.Rename(dir, dir+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("block persistence"), 0600); err != nil {
		t.Fatal(err)
	}
	fresh := rawCredential("rotated", "user", "cn", true)
	a := &credentialAdapter{refresh: func(context.Context, json.RawMessage) (json.RawMessage, error) { return fresh, nil }}
	if _, err := service.Resolve(t.Context(), "a", a); err == nil {
		t.Fatal("save failure was hidden")
	}
	if !bytes.Equal(st.Account("a").Credentials, fresh) {
		t.Fatal("save failure restored an invalidated token")
	}
	if raw, err := service.Resolve(t.Context(), "a", a); err != nil || !bytes.Equal(raw, fresh) || a.refreshes.Load() != 1 {
		t.Fatal("save failure caused refresh replay", err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"-saved", dir); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Open(st.Path())
	if err != nil || !sameCredentialJSON(reloaded.Account("a").Credentials, fresh) {
		t.Fatal("rotated token could not be recovered", err)
	}
}

func TestProviderCredentialsCanceledRefreshKeepsOldGenerationAndReleasesLock(t *testing.T) {
	st, service := credentialStore(t, false)
	before := st.Account("a").Credentials
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a := &credentialAdapter{refresh: func(got context.Context, raw json.RawMessage) (json.RawMessage, error) {
		if got == ctx {
			close(entered)
			<-got.Done()
			return nil, got.Err()
		}
		return rawCredential("rotated", "user", "cn", true), nil
	}}
	done := make(chan error, 1)
	go func() { _, err := service.Resolve(ctx, "a", a); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || !bytes.Equal(before, st.Account("a").Credentials) {
		t.Fatal("canceled refresh committed credentials or lost cancellation", err)
	}
	next, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if _, err := service.Resolve(next, "a", a); err != nil || a.refreshes.Load() != 2 {
		t.Fatal("canceled refresh did not release the account lock", err)
	}
}

func TestProviderCredentialsPreserveErrorAndIdentityBoundaries(t *testing.T) {
	upstreamErr := errors.New("fixture refresh failure")
	for _, scenario := range []string{"malformed", "wrong-user", "wrong-scope", "refresh-error", "malformed-refresh", "changed-user", "changed-scope"} {
		t.Run(scenario, func(t *testing.T) {
			st, service := credentialStore(t, false)
			acct := st.Account("a")
			switch scenario {
			case "malformed":
				acct.Credentials = json.RawMessage(`{`)
			case "wrong-user":
				acct.Credentials = rawCredential("original", "wrong", "cn", false)
			case "wrong-scope":
				acct.Credentials = rawCredential("original", "user", "global", false)
			}
			st.UpsertAccount(acct)
			before := st.Account("a").Credentials
			a := &credentialAdapter{refresh: func(context.Context, json.RawMessage) (json.RawMessage, error) {
				switch scenario {
				case "refresh-error":
					return nil, upstreamErr
				case "malformed-refresh":
					return json.RawMessage(`{`), nil
				case "changed-user":
					return rawCredential("rotated", "wrong", "cn", true), nil
				case "changed-scope":
					return rawCredential("rotated", "user", "global", true), nil
				default:
					t.Error("invalid credentials reached refresh")
					return nil, upstreamErr
				}
			}}
			_, err := service.Resolve(t.Context(), "a", a)
			if err == nil || !bytes.Equal(before, st.Account("a").Credentials) {
				t.Fatal("failed or mismatched credentials were committed", err)
			}
			if scenario == "refresh-error" && !errors.Is(err, upstreamErr) {
				t.Fatal("upstream error identity was lost", err)
			}
			if scenario == "wrong-user" || scenario == "wrong-scope" {
				var fault *provider.Error
				if !errors.As(err, &fault) || fault.Status != 401 || !fault.AccountFault || a.refreshes.Load() != 0 {
					t.Fatal("identity validation changed", err)
				}
			}
		})
	}
}
