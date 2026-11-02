package storage

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/access"
)

func enableTestWallet(t *testing.T, s *Store, u access.UserView, amount string) {
	t.Helper()
	n, err := access.ParseCredits(amount)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AdjustWallet(u.ID, 0, n, true, false, "acceptance grant"); err != nil {
		t.Fatal(err)
	}
}
func TestConsumerRedemptionConcurrentAtomicAndRestart(t *testing.T) {
	s, u, _ := userFixture(t, KeyBudget{})
	codes, err := s.GenerateRedemptions(access.Redemption{Kind: "balance", Amount: 123456789, ExpiresAt: time.Now().Add(time.Hour).Unix()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Redeem(u.ID, codes[0]) == nil {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("code accepted %d times", count.Load())
	}
	got, _ := s.User(u.ID)
	if got.Wallet.Balance != 123456789 || !got.Wallet.Enabled || len(s.UserLedger(u.ID)) != 1 {
		t.Fatal("grant not atomic", got.Wallet)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), codes[0]) {
		t.Fatal("plaintext code persisted")
	}
	again := reopenJournal(t, s)
	if again.Redeem(u.ID, codes[0]) == nil {
		t.Fatal("used code accepted after restart")
	}
	if again.Redemptions()[0].Digest != "" {
		t.Fatal("code digest exposed")
	}
}
func TestConsumerWriteFailureRollsBackCodeAndGrant(t *testing.T) {
	s, u, _ := userFixture(t, KeyBudget{})
	codes, err := s.GenerateRedemptions(access.Redemption{Kind: "balance", Amount: 100, ExpiresAt: time.Now().Add(time.Hour).Unix()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	path := s.path
	s.path = filepath.Join(t.TempDir(), "missing", "state.json") // writeState creates parent, so use a regular file as parent.
	blocker := filepath.Join(t.TempDir(), "block")
	if err = os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	if s.Redeem(u.ID, codes[0]) == nil {
		t.Fatal("failed save accepted")
	}
	got, _ := s.User(u.ID)
	if got.Wallet.Balance != 0 || s.Redemptions()[0].UsedBy != "" || len(s.UserLedger(u.ID)) != 0 {
		t.Fatal("rollback lost atomicity")
	}
	s.path = path
	if err = s.Redeem(u.ID, codes[0]); err != nil {
		t.Fatal("failed save burned code", err)
	}
}
func TestConsumerWalletReservationSettlementAndReplay(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	enableTestWallet(t, s, u, "0.01")
	if err := s.SetPriceAndSave("test-priced", ModelPrice{Currency: "USD", Output: 1}); err != nil {
		t.Fatal(err)
	}
	release, metered, err := s.BeginRequestBudget(k.Key)
	if err != nil || !metered {
		t.Fatal(err, metered)
	}
	if err = s.ReserveConsumerQuote(k.Key, "one", "test-priced", 0, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveConsumerQuote(k.Key, "two", "test-priced", 0, nil); err == nil {
		t.Fatal("concurrent reservation overspent")
	}
	if err = s.ReserveConsumerQuote(k.Key, "one", "test-priced", 0, nil); err != nil {
		t.Fatal("retry double-reserved", err)
	}
	if s.UserReservedCredits(u.ID) != 819200 {
		t.Fatal("unexpected quote", s.UserReservedCredits(u.ID))
	}
	if err = s.AdjustWallet(u.ID, 1, 1, true, false, "inflight change"); err == nil {
		t.Fatal("inflight wallet mutation accepted")
	}
	e := LogEntry{RequestID: "one", UserID: u.ID, Time: time.Now().Unix(), Attempts: 1, Status: 200, Model: "test-priced", Usage: TokenUsage{Known: true, OutputTokens: 1000}}
	if err = s.Record(e, 1, k.Key); err != nil {
		t.Fatal(err)
	}
	release()
	s.ReleaseConsumerQuote("one")
	s.ReleaseConsumerQuote("one")
	got, _ := s.User(u.ID)
	if got.Wallet.Balance != 900000 || s.UserReservedCredits(u.ID) != 0 {
		t.Fatal("wrong settlement or leaked hold", got.Wallet)
	}
	// Recover from a checkpoint written before the journal append, then replay twice.
	again := reopenJournal(t, s)
	recovered, _ := again.User(u.ID)
	if recovered.Wallet != got.Wallet {
		t.Fatal("replay repriced/double charged", recovered.Wallet, got.Wallet)
	}
	again = reopenJournal(t, again)
	recovered, _ = again.User(u.ID)
	if recovered.Wallet != got.Wallet {
		t.Fatal("checkpoint double charged")
	}
	if err = s.ReserveConsumerQuote(k.Key, "abort", "test-priced", 0, nil); err != nil {
		t.Fatal(err)
	}
	s.ReleaseConsumerQuote("abort")
	recovered, _ = s.User(u.ID)
	if recovered.Wallet.Balance != got.Wallet.Balance || s.UserReservedCredits(u.ID) != 0 {
		t.Fatal("abort charged")
	}
}
func TestConsumerMissingUsagePausesWalletAndLegacyUnchanged(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	release, metered, err := s.BeginRequestBudget(k.Key)
	if err != nil || metered {
		t.Fatal("legacy wallet enabled", err, metered)
	}
	release()
	enableTestWallet(t, s, u, "10")
	if err = s.Record(LogEntry{UserID: u.ID, RequestID: "missing-usage", Attempts: 1, Time: time.Now().Unix(), Status: 500}, 0, k.Key); err != nil {
		t.Fatal(err)
	}
	got, _ := s.User(u.ID)
	if !got.Wallet.ReviewRequired || got.Wallet.Balance != 1000000000 {
		t.Fatal("unknown usage charged zero silently", got.Wallet)
	}
	if _, err = s.BeginKeyBudget(k.Key); err == nil {
		t.Fatal("unverified wallet continued")
	}
	if err = s.AdjustWallet(u.ID, got.Wallet.Revision, 0, true, true, "manually reconciled"); err != nil {
		t.Fatal(err)
	}
	release, err = s.BeginKeyBudget(k.Key)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err = s.AdjustWallet(u.ID, got.Wallet.Revision, 1, true, false, "duplicate request"); err == nil {
		t.Fatal("stale grant retried twice")
	}
}
func TestConsumerSubscriptionQuotaRenewalExpiryAndTemplateIsolation(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	p, err := s.SavePlan(access.Plan{Name: "one request", GroupID: u.GroupID, Enabled: true, Days: 30, MaxConcurrency: 2, Period: "month", Budget: KeyBudget{MaxRequests: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ManageSubscription(u.ID, p.ID, "assign", 0); err != nil {
		t.Fatal(err)
	}
	release, err := s.BeginKeyBudget(k.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginKeyBudget(k.Key); err == nil {
		t.Fatal("request quota oversubscribed")
	}
	if err = s.Record(LogEntry{UserID: u.ID, Attempts: 1, Time: time.Now().Unix(), Status: 200, Usage: TokenUsage{Known: true}}, 0, k.Key); err != nil {
		t.Fatal(err)
	}
	release()
	old, _ := s.User(u.ID)
	if err = s.ManageSubscription(u.ID, p.ID, "assign", 3); err != nil {
		t.Fatal(err)
	}
	got, _ := s.User(u.ID)
	if got.Subscription.Usage.Requests != 1 || got.Subscription.ID != old.Subscription.ID || got.Subscription.ExpiresAt != old.Subscription.ExpiresAt+3*86400 {
		t.Fatal("renewal reset quota")
	}
	if _, err = s.BeginKeyBudget(k.Key); err == nil {
		t.Fatal("renewal granted quota reset")
	}
	// Roll to a fresh UTC period without waiting for a wall-clock boundary.
	s.mu.Lock()
	s.userLocked(u.ID).Subscription.PeriodStart = 1
	s.mu.Unlock()
	release, err = s.BeginKeyBudget(k.Key)
	if err != nil {
		t.Fatal("new period did not reset quota", err)
	}
	release()
	s.mu.Lock()
	s.userLocked(u.ID).Subscription.ExpiresAt = time.Now().Unix() - 1
	s.mu.Unlock()
	if s.ValidKey(k.Key) {
		t.Fatal("expired subscription authenticated")
	}
	if _, err = s.BeginKeyBudget(k.Key); err == nil {
		t.Fatal("expired subscription admitted")
	}
	if err = s.ManageSubscription(u.ID, p.ID, "assign", 1); err != nil {
		t.Fatal(err)
	}
	if !s.ValidKey(k.Key) {
		t.Fatal("renewal did not restore access")
	}
	if err = s.ManageSubscription(u.ID, p.ID, "disable", 0); err != nil {
		t.Fatal(err)
	}
	if s.ValidKey(k.Key) {
		t.Fatal("disabled subscription authenticated")
	}
	if err = s.ManageSubscription(u.ID, "", "remove", 0); err != nil {
		t.Fatal(err)
	}
	if !s.ValidKey(k.Key) {
		t.Fatal("admin removal did not restore base policy")
	}
}

type fakeConsumerIdentityTransport struct {
	Code    string
	Subject string
	Sends   int
}

func (f *fakeConsumerIdentityTransport) Send(_ context.Context, _ access.IdentitySettings, _ string, code string) error {
	f.Code = code
	f.Sends++
	return nil
}
func (f *fakeConsumerIdentityTransport) Exchange(context.Context, access.IdentitySettings, string, string, string, string) (string, error) {
	return f.Subject, nil
}
func TestConsumerEmailRecoveryAndOAuthOwnership(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	cfg := access.IdentitySettings{PublicOrigin: "https://panel.example", SMTPHost: "smtp.example", SMTPPort: 587, SMTPFrom: "noreply@example.com", GitHub: access.OAuthSettings{Enabled: true, ClientID: "test", ClientSecret: "private"}}
	if err := s.SaveIdentitySettings(cfg, false, false, false); err != nil {
		t.Fatal(err)
	}
	users := access.NewUsers(s)
	transport := &fakeConsumerIdentityTransport{Subject: "123"}
	identity := access.NewIdentity(s, users, transport)
	ctx := context.Background()
	token, err := identity.StartEmail(ctx, u.ID, "Alice@Example.com", "verify")
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.VerifyEmail("another-user", token, transport.Code); err == nil {
		t.Fatal("email proof crossed identity")
	}
	if err = identity.VerifyEmail(u.ID, token, transport.Code); err != nil {
		t.Fatal(err)
	}
	if err = identity.VerifyEmail(u.ID, token, transport.Code); err == nil {
		t.Fatal("email proof reused")
	}
	got, _ := s.User(u.ID)
	if got.Email != "alice@example.com" || !got.EmailVerified {
		t.Fatal("email not normalized/verified")
	}
	if _, _, err = users.OpenSession(u.ID, got.Revision, "session-before-reset"); err != nil {
		t.Fatal(err)
	}
	token, err = identity.StartEmail(ctx, "", "alice@example.com", "reset")
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.ResetPassword(ctx, token, transport.Code, "new-password-123456"); err != nil {
		t.Fatal(err)
	}
	if _, ok := users.Authenticate("session-before-reset"); ok {
		t.Fatal("reset did not revoke sessions")
	}
	if !s.ValidKey(k.Key) {
		t.Fatal("password reset destroyed API key")
	}
	if err = identity.ResetPassword(ctx, token, transport.Code, "new-password-123456"); err == nil {
		t.Fatal("reset proof reused")
	}
	unknown, err := identity.StartEmail(ctx, "", "unknown@example.com", "reset")
	if err != nil || unknown == "" || transport.Sends != 2 {
		t.Fatal("unknown-email disclosure or unwanted mail", err, transport.Sends)
	}
	target, err := identity.StartOAuth("github", u.ID, "browser")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	state := parsed.Query().Get("state")
	if parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("PKCE missing")
	}
	if _, _, err = identity.CompleteOAuth(ctx, state, "code", "other-browser"); err == nil {
		t.Fatal("OAuth browser correlation bypassed")
	}
	bound, linked, err := identity.CompleteOAuth(ctx, state, "code", "browser")
	if err != nil || !linked || bound.ID != u.ID {
		t.Fatal("binding failed", err)
	}
	if _, _, err = identity.CompleteOAuth(ctx, state, "code", "browser"); err == nil {
		t.Fatal("OAuth state reused")
	}
	target, err = identity.StartOAuth("github", "", "browser")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ = url.Parse(target)
	logged, linked, err := identity.CompleteOAuth(ctx, parsed.Query().Get("state"), "code", "browser")
	if err != nil || linked || logged.ID != u.ID {
		t.Fatal("bound login failed", err)
	}
	transport.Subject = "unbound"
	target, _ = identity.StartOAuth("github", "", "browser")
	parsed, _ = url.Parse(target)
	if _, _, err = identity.CompleteOAuth(ctx, parsed.Query().Get("state"), "code", "browser"); err == nil {
		t.Fatal("unbound identity automatically joined user/group")
	}
	got, _ = s.User(u.ID)
	if err = s.UnbindIdentity(u.ID, got.Revision, "github"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ExternalUser("github", "123"); ok {
		t.Fatal("unbind lost")
	}
}

func TestConsumerConcurrencyCodeRaisesEffectivePlanCap(t *testing.T) {
	s, u, k := userFixture(t, KeyBudget{})
	p, err := s.SavePlan(access.Plan{Name: "limited", GroupID: u.GroupID, Enabled: true, Days: 30, MaxConcurrency: 1, Period: "lifetime"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ManageSubscription(u.ID, p.ID, "assign", 0); err != nil {
		t.Fatal(err)
	}
	codes, err := s.GenerateRedemptions(access.Redemption{Kind: "concurrency", Concurrency: 2, ExpiresAt: time.Now().Add(time.Hour).Unix()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Redeem(u.ID, codes[0]); err != nil {
		t.Fatal(err)
	}
	got, _ := s.User(u.ID)
	if effectiveConcurrency(&got) != 3 {
		t.Fatal("code had no effect under subscription", got)
	}
	releases := []func(){}
	for range 3 {
		release, err := s.BeginKeyBudget(k.Key)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	if _, err = s.BeginKeyBudget(k.Key); err == nil {
		t.Fatal("effective plan cap ignored")
	}
	for _, release := range releases {
		release()
	}
}

func TestConsumerMalformedSubscriptionNeverBecomesUnlimited(t *testing.T) {
	s, u, _ := userFixture(t, KeyBudget{})
	p, err := s.SavePlan(access.Plan{Name: "valid", Enabled: true, Days: 1, GroupID: u.GroupID, Period: "day", MaxConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ManageSubscription(u.ID, p.ID, "assign", 0); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.userLocked(u.ID).Subscription.Plan.MaxConcurrency = -1
	s.mu.Unlock()
	if err = s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(s.path); err == nil {
		t.Fatal("malformed persisted subscription enabled unlimited access")
	}
}
