package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/provider"
)

type loginMemoryStore struct {
	mu       sync.Mutex
	accounts map[string]*Account
	calls    int
	fail     bool
}

func (s *loginMemoryStore) Account(id string) *Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accounts[id]
}
func (s *loginMemoryStore) FindProviderAccount(p, scope, id string) *Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.accounts {
		if a.Provider == p && a.Scope == scope && a.ExternalID == id {
			return a
		}
	}
	return nil
}
func (s *loginMemoryStore) UpsertAccountAndSave(a *Account, replace bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.fail {
		return false, errors.New("fixture persistence failure")
	}
	for id, old := range s.accounts {
		if old.Provider == a.Provider && old.Scope == a.Scope && old.ExternalID == a.ExternalID {
			if !replace {
				return false, errors.New("fixture duplicate")
			}
			a.ID = id
		}
	}
	copy := *a
	copy.Credentials = append(json.RawMessage(nil), a.Credentials...)
	s.accounts[a.ID] = &copy
	return true, nil
}

type loginAdapterFixture struct {
	identity         provider.Identity
	ready            bool
	checks, releases atomic.Int64
	begin            func(context.Context, string) (provider.Login, error)
	exchange         func(context.Context) (json.RawMessage, bool, error)
	verify           func(context.Context, json.RawMessage) (provider.Identity, error)
	normalizeError   error
}

func (a *loginAdapterFixture) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "fixture"}
}
func (a *loginAdapterFixture) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	return raw, a.identity, a.normalizeError
}
func (*loginAdapterFixture) Refresh(context.Context, json.RawMessage) (json.RawMessage, error) {
	panic("unexpected refresh")
}
func (*loginAdapterFixture) Stream(context.Context, json.RawMessage, provider.Request, func([]byte) error) error {
	panic("unexpected stream")
}
func (*loginAdapterFixture) Models(context.Context, json.RawMessage) ([]provider.Model, error) {
	panic("unexpected models")
}
func (*loginAdapterFixture) Credit(context.Context, json.RawMessage) (json.RawMessage, error) {
	panic("unexpected credit")
}
func (a *loginAdapterFixture) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	if a.begin != nil {
		return a.begin(ctx, scope)
	}
	return provider.Login{State: "fixture-state", URL: "https://fixture.example/login"}, nil
}
func (a *loginAdapterFixture) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	a.checks.Add(1)
	if a.exchange != nil {
		return a.exchange(ctx)
	}
	return json.RawMessage(`{"token":"fixture-secret"}`), a.ready, nil
}
func (a *loginAdapterFixture) ReleaseLogin(string) { a.releases.Add(1) }
func (a *loginAdapterFixture) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	if a.verify != nil {
		return a.verify(ctx, raw)
	}
	return a.identity, nil
}

type callbackLoginFixture struct{ *loginAdapterFixture }

func (a callbackLoginFixture) CheckCallback(ctx context.Context, scope, state, input string) (json.RawMessage, bool, error) {
	return a.CheckLogin(ctx, scope, state)
}

type deviceLoginFixture struct{ *loginAdapterFixture }

func (a deviceLoginFixture) BeginLoginWithDevice(ctx context.Context, scope, machine, device string) (provider.Login, error) {
	if machine != "fixture-machine" || device != "fixture-device" {
		return provider.Login{}, errors.New("device required")
	}
	return a.BeginLogin(ctx, scope)
}
func loginFixture() (*ProviderLogins, *loginMemoryStore, *loginAdapterFixture, *provider.Registry) {
	st := &loginMemoryStore{accounts: map[string]*Account{}}
	var ids atomic.Int64
	svc := NewProviderLogins(st, func(n int) string { return fmt.Sprintf("%d-%d", n, ids.Add(1)) })
	adapter := &loginAdapterFixture{identity: provider.Identity{ID: "fixture-user", Name: "Fixture", Scope: "cn", ExpiresAt: time.Now().Add(time.Hour).Unix(), HasRefresh: true}, ready: true}
	reg := provider.NewRegistry()
	_ = reg.Register(adapter)
	return svc, st, adapter, reg
}
func loginStart(t *testing.T, s *ProviderLogins, reg LoginAdapters) string {
	t.Helper()
	r, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "start", Provider: "fixture"}, reg)
	if err != nil || r.State != LoginStarted || r.Scope != "cn" || r.ExpiresAt < time.Now().Add(9*time.Minute).Unix() {
		t.Fatalf("start: %+v %v", r, err)
	}
	return r.SessionID
}
func loginKind(t *testing.T, err error, want LoginErrorKind) {
	t.Helper()
	var failure *LoginError
	if !errors.As(err, &failure) || failure.Kind != want {
		t.Fatalf("want error kind %v, got %v", want, err)
	}
}

func TestProviderLoginsConcurrentPreviewAndSaveDoNotReplay(t *testing.T) {
	s, st, a, reg := loginFixture()
	id := loginStart(t, s, reg)
	entered, resume := make(chan struct{}), make(chan struct{})
	a.exchange = func(ctx context.Context) (json.RawMessage, bool, error) {
		close(entered)
		select {
		case <-resume:
			return json.RawMessage(`{"token":"fixture-secret"}`), true, nil
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	const count = 12
	results := make(chan ProviderLoginResult, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		go func() {
			r, e := s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id}, reg)
			results <- r
			errs <- e
		}()
	}
	<-entered
	close(resume)
	var preview string
	for i := 0; i < count; i++ {
		r := <-results
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if r.State != LoginPreview || r.Existing {
			t.Fatalf("preview: %+v", r)
		}
		if preview != "" && preview != r.PreviewID {
			t.Fatal("preview changed")
		}
		preview = r.PreviewID
	}
	if a.checks.Load() != 1 || len(st.accounts) != 0 {
		t.Fatal("preview replayed or persisted")
	}
	for i := 0; i < count; i++ {
		go func() {
			r, e := s.Apply(t.Context(), ProviderLoginRequest{Action: "complete", SessionID: id, Preview: preview, Name: "  saved name  "}, reg)
			results <- r
			errs <- e
		}()
	}
	var saved string
	for i := 0; i < count; i++ {
		r := <-results
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if r.State != LoginSaved || (saved != "" && saved != r.AccountID) {
			t.Fatal("save replay changed account")
		}
		saved = r.AccountID
	}
	if st.calls != 1 || len(st.accounts) != 1 || a.releases.Load() != 1 || st.Account(saved).Name != "saved name" || s.flows[id].credentials != nil {
		t.Fatal("save repeated or preview credentials retained")
	}
	_, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id}, reg)
	loginKind(t, err, LoginConflict)
	st.mu.Lock()
	delete(st.accounts, saved)
	st.mu.Unlock()
	_, err = s.Apply(t.Context(), ProviderLoginRequest{Action: "complete", SessionID: id, Preview: preview}, reg)
	loginKind(t, err, LoginExpired)
	if st.calls != 1 {
		t.Fatal("deleted account resurrected")
	}
}

func TestProviderLoginsDuplicateAndFailedSaveKeepConfirmedPreview(t *testing.T) {
	s, st, a, reg := loginFixture()
	st.accounts["existing"] = &Account{ID: "existing", Provider: "fixture", Scope: "cn", ExternalID: "fixture-user"}
	id := loginStart(t, s, reg)
	r, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id}, reg)
	if err != nil || !r.Existing {
		t.Fatal("existing identity not reported", err)
	}
	request := ProviderLoginRequest{Action: "complete", SessionID: id, Preview: "wrong"}
	_, err = s.Apply(t.Context(), request, reg)
	loginKind(t, err, LoginConflict)
	if st.calls != 0 {
		t.Fatal("unconfirmed preview saved")
	}
	request.Preview = r.PreviewID
	_, err = s.Apply(t.Context(), request, reg)
	loginKind(t, err, LoginConflict)
	request.Replace = true
	st.fail = true
	_, err = s.Apply(t.Context(), request, reg)
	loginKind(t, err, LoginConflict)
	if a.releases.Load() != 0 || len(s.flows[id].credentials) == 0 {
		t.Fatal("failed save discarded authorization")
	}
	st.fail = false
	r, err = s.Apply(t.Context(), request, reg)
	if err != nil || r.AccountID != "existing" || len(st.accounts) != 1 || a.checks.Load() != 1 || a.releases.Load() != 1 {
		t.Fatal("replacement failed or repeated exchange", err)
	}
}

func TestProviderLoginsPendingCallbackAndCancellation(t *testing.T) {
	s, st, a, _ := loginFixture()
	reg := provider.NewRegistry()
	_ = reg.Register(callbackLoginFixture{a})
	a.ready = false
	id := loginStart(t, s, reg)
	request := ProviderLoginRequest{Action: "check", SessionID: id, Input: "fixture-callback-A"}
	r, err := s.Apply(t.Context(), request, reg)
	if err != nil || r.State != LoginPending || len(st.accounts) != 0 {
		t.Fatal("pending mutated accounts", err)
	}
	a.ready = true
	r, err = s.Apply(t.Context(), request, reg)
	if err != nil || r.State != LoginPreview {
		t.Fatal(err)
	}
	request.Input = " fixture-callback-A "
	if _, err = s.Apply(t.Context(), request, reg); err != nil {
		t.Fatal(err)
	}
	request.Input = "fixture-callback-B"
	_, err = s.Apply(t.Context(), request, reg)
	loginKind(t, err, LoginConflict)
	request.Input = ""
	if _, err = s.Apply(t.Context(), request, reg); err != nil {
		t.Fatal(err)
	}
	if a.checks.Load() != 2 {
		t.Fatal("callback re-exchanged")
	}
	_, err = s.Apply(t.Context(), ProviderLoginRequest{Action: "cancel", SessionID: id}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if s.flows[id].credentials != nil || a.releases.Load() != 1 {
		t.Fatal("cancel retained credentials")
	}
	_, err = s.Apply(t.Context(), ProviderLoginRequest{Action: "complete", SessionID: id, Preview: r.PreviewID}, reg)
	loginKind(t, err, LoginExpired)
	if len(st.accounts) != 0 {
		t.Fatal("cancel saved an account")
	}
}

func TestProviderLoginsSessionLimitsExpiryAndValidation(t *testing.T) {
	s, st, a, reg := loginFixture()
	id := loginStart(t, s, reg)
	for _, input := range []string{"http://localhost/?state=other", "ftp://localhost/?state=fixture-state", "http://user@localhost/?state=fixture-state", "%"} {
		_, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id, Input: input}, reg)
		loginKind(t, err, LoginInvalid)
	}
	if a.checks.Load() != 0 {
		t.Fatal("bad callback reached exchange")
	}
	a.identity.Scope = "global"
	_, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id}, reg)
	loginKind(t, err, LoginUpstream)
	a.identity.Scope = "cn"
	a.identity.ExpiresAt = time.Now().Add(-time.Second).Unix()
	r, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id}, reg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Apply(t.Context(), ProviderLoginRequest{Action: "complete", SessionID: id, Preview: r.PreviewID}, reg)
	loginKind(t, err, LoginExpired)
	if st.calls != 0 {
		t.Fatal("expired credential saved")
	}
	_, err = s.Apply(t.Context(), ProviderLoginRequest{Action: "unknown", SessionID: id}, reg)
	loginKind(t, err, LoginInvalid)
	for i := 1; i < 128; i++ {
		loginStart(t, s, reg)
	}
	_, err = s.Apply(t.Context(), ProviderLoginRequest{Action: "start", Provider: "fixture"}, reg)
	loginKind(t, err, LoginCapacity)
	s.flows[id].created = time.Now().Add(-11 * time.Minute)
	_, err = s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id}, reg)
	loginKind(t, err, LoginExpired)
	loginStart(t, s, reg)
	if a.releases.Load() != 1 || len(s.flows) != 128 {
		t.Fatal("expired session not cleaned up")
	}
	_, err = s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id}, reg)
	loginKind(t, err, LoginMissing)
}

func TestProviderLoginsUseCurrentAdaptersDeviceAndContext(t *testing.T) {
	s, _, a, reg := loginFixture()
	id := loginStart(t, s, reg)
	replacement := &loginAdapterFixture{identity: a.identity, ready: true}
	current := provider.NewRegistry()
	_ = current.Register(replacement)
	if _, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "check", SessionID: id}, current); err != nil {
		t.Fatal(err)
	}
	if a.checks.Load() != 0 || replacement.checks.Load() != 1 {
		t.Fatal("cached older registry")
	}
	device := provider.NewRegistry()
	_ = device.Register(deviceLoginFixture{a})
	_, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "start", Provider: "fixture"}, device)
	loginKind(t, err, LoginInvalid)
	r, err := s.Apply(t.Context(), ProviderLoginRequest{Action: "start", Provider: "fixture", MachineID: "fixture-machine", DeviceID: "fixture-device"}, device)
	if err != nil || r.State != LoginStarted {
		t.Fatal("device login failed", err)
	}
	a.begin = func(ctx context.Context, scope string) (provider.Login, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Error("missing login timeout")
		}
		return provider.Login{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.Apply(ctx, ProviderLoginRequest{Action: "start", Provider: "fixture"}, reg)
	loginKind(t, err, LoginUpstream)
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatal(err)
	}
}

func TestProviderImportVerifiesBeforeSaveAndPreservesReplacement(t *testing.T) {
	s, st, a, reg := loginFixture()
	request := ProviderImportRequest{Provider: "fixture", Credentials: json.RawMessage(`{"token":"fixture-import"}`)}
	verified := a.identity
	verified.ID = "another-user"
	a.verify = func(context.Context, json.RawMessage) (provider.Identity, error) { return verified, nil }
	_, err := s.Import(t.Context(), request, reg)
	loginKind(t, err, LoginInvalid)
	verified = a.identity
	verified.Scope = "global"
	_, err = s.Import(t.Context(), request, reg)
	loginKind(t, err, LoginInvalid)
	if st.calls != 0 {
		t.Fatal("unverified import persisted")
	}
	verified = a.identity
	verified.Name = "Verified name"
	a.verify = func(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Error("missing import timeout")
		}
		if ctx.Err() != nil {
			return provider.Identity{}, ctx.Err()
		}
		return verified, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.Import(ctx, request, reg)
	loginKind(t, err, LoginInvalid)
	id, err := s.Import(t.Context(), request, reg)
	if err != nil || st.Account(id).Name != "Verified name" || st.Account(id).Source != "session" {
		t.Fatal("verified import failed", err)
	}
	_, err = s.Import(t.Context(), request, reg)
	loginKind(t, err, LoginConflict)
	request.Replace = true
	request.Name = "Explicit name"
	saved, err := s.Import(t.Context(), request, reg)
	if err != nil || id != saved || len(st.accounts) != 1 || st.Account(id).Name != "Explicit name" {
		t.Fatal("import replacement failed", err)
	}
	calls := st.calls
	a.identity.ExpiresAt = time.Now().Add(-time.Second).Unix()
	_, err = s.Import(t.Context(), request, reg)
	loginKind(t, err, LoginInvalid)
	if st.calls != calls {
		t.Fatal("expired import reached store")
	}
}
