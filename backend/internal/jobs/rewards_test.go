package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

type rewardAdapter struct {
	provider.Adapter
	claim  func(context.Context) (provider.RewardResult, error)
	status func(context.Context) (provider.RewardResult, error)
}

func (a rewardAdapter) ClaimDaily(ctx context.Context, _ json.RawMessage) (provider.RewardResult, error) {
	return a.claim(ctx)
}
func (a rewardAdapter) DailyStatus(ctx context.Context, _ json.RawMessage) (provider.RewardResult, error) {
	return a.status(ctx)
}

type failingRewardStore struct {
	*storage.Store
	fail bool
}

func (s *failingRewardStore) SaveDailyReward(id, date, state, msg string, ok bool, next int64) error {
	if s.fail {
		return errors.New("disk failure")
	}
	return s.Store.SaveDailyReward(id, date, state, msg, ok, next)
}
func (s *failingRewardStore) Save() error {
	if s.fail {
		return errors.New("disk failure")
	}
	return s.Store.Save()
}
func rewardFixture(t *testing.T) (*failingRewardStore, *Rewards, *accounts.Credits) {
	t.Helper()
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.UpsertAccount(&accounts.Account{ID: "wb", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true})
	fs := &failingRewardStore{Store: st}
	c := &accounts.Credits{}
	return fs, NewRewards(fs, c.Invalidate, func(string, ...any) {}), c
}
func TestRewardsCancellationRecheckAndLockIsolation(t *testing.T) {
	st, r, _ := rewardFixture(t)
	release, err := r.locks.Acquire(t.Context(), "wb")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	ops := RewardOperations{Provider: func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error) {
		calls.Add(1)
		return nil, nil, errors.New("must not resolve")
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.Claim(ctx, st.Account("wb"), "", ops); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stale := st.Account("wb")
	done := make(chan error, 1)
	go func() { _, err := r.Claim(t.Context(), stale, "", ops); done <- err }()
	st.SetAccountEnabled("wb", false)
	release()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "停用") {
		t.Fatal("latest enablement not checked", err)
	}
	release, err = r.locks.Acquire(t.Context(), "wb")
	if err != nil {
		t.Fatal(err)
	}
	if other, err := r.locks.Acquire(t.Context(), "other"); err != nil {
		t.Fatal(err)
	} else {
		other()
	}
	release()
	if calls.Load() != 0 {
		t.Fatal("canceled or disabled account reached credentials")
	}
}
func TestRewardsConfirmationPersistenceAndCacheOrder(t *testing.T) {
	for _, mode := range []string{"claimed", "unknown", "upstream-error", "save-error", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			st, r, c := rewardFixture(t)
			c.Store("wb", &accounts.CreditInfo{Remaining: 10}, nil)
			claims := 0
			ops := RewardOperations{Provider: func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error) {
				if mode == "unsupported" {
					return creditOnlyAdapter{}, nil, nil
				}
				return rewardAdapter{claim: func(context.Context) (provider.RewardResult, error) {
					claims++
					if mode == "upstream-error" {
						return provider.RewardResult{}, errors.New("upstream failed")
					}
					return provider.RewardResult{State: map[bool]string{true: "claimed", false: "unknown"}[mode == "claimed" || mode == "save-error"], Message: "fixture result"}, nil
				}}, nil, nil
			}}
			st.fail = mode == "save-error"
			_, err := r.Claim(t.Context(), st.Account("wb"), "ignored-external-date", ops)
			if (err == nil) != (mode == "claimed") {
				t.Fatalf("mode %s error %v", mode, err)
			}
			a := st.Account("wb")
			_, _, cached := c.Cached("wb")
			if mode == "save-error" || mode == "unsupported" {
				if !cached || a.LastClaimDate != "" || a.NextClaimAt != 0 {
					t.Fatal("failed save/unsupported changed state or invalidated cache")
				}
				return
			}
			if cached {
				t.Fatal("cache not invalidated after saved outcome")
			}
			if mode == "claimed" {
				if a.LastClaimDate != DateFor(a, time.Now()) || a.NextClaimAt != 0 {
					t.Fatal("confirmation missing")
				}
			} else {
				if a.LastClaimDate != "" || a.NextClaimAt <= time.Now().Add(29*time.Minute).Unix() {
					t.Fatal("unconfirmed reward missing cooldown")
				}
			}
			_, _ = r.Claim(t.Context(), st.Account("wb"), "", ops)
			if claims != 1 {
				t.Fatal("confirmed/cooldown repeated upstream")
			}
			if a.Failures != 0 {
				t.Fatal("reward failure poisoned inference health")
			}
		})
	}
}

type creditOnlyAdapter struct{ provider.Adapter }

func TestRewardStatusAndNativeLegacySaveFailure(t *testing.T) {
	st, r, c := rewardFixture(t)
	c.Store("wb", &accounts.CreditInfo{Remaining: 10}, nil)
	ops := RewardOperations{Provider: func(context.Context, *accounts.Account) (provider.Adapter, json.RawMessage, error) {
		return rewardAdapter{status: func(context.Context) (provider.RewardResult, error) {
			return provider.RewardResult{State: "already_claimed"}, nil
		}, claim: func(context.Context) (provider.RewardResult, error) {
			t.Fatal("status claimed reward")
			return provider.RewardResult{}, nil
		}}, nil, nil
	}}
	st.fail = true
	if _, err := r.Status(t.Context(), st.Account("wb"), ops); err == nil {
		t.Fatal("confirmed status hid save failure")
	}
	st.fail = false
	if _, err := r.Status(t.Context(), st.Account("wb"), ops); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Cached("wb"); !ok {
		t.Fatal("read-only status changed credit cache")
	}
	st.UpsertAccount(&accounts.Account{ID: "ca", Enabled: true})
	st.fail = true
	logs := 0
	r.logf = func(string, ...any) { logs++ }
	ops.Native = func(context.Context, *accounts.Account) (string, error) { return "native success", nil }
	if msg, err := r.Claim(t.Context(), st.Account("ca"), "2000-01-02", ops); err != nil || msg != "native success" {
		t.Fatal(msg, err)
	}
	if st.Account("ca").LastClaimDate != "2000-01-02" || logs != 1 {
		t.Fatal("native legacy save policy changed")
	}
	ops.Native = func(context.Context, *accounts.Account) (string, error) { return "", errors.New("credential error") }
	if msg, err := r.Claim(t.Context(), st.Account("ca"), "2000-01-03", ops); err == nil || msg != "" || st.Account("ca").LastClaimErr != "credential error" {
		t.Fatal("native credential error record changed")
	}
}
func TestRewardDateAndWindowPreserveProviderSemantics(t *testing.T) {
	now := time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)
	if DateFor(&accounts.Account{}, now) != "2026-11-01" || DateFor(&accounts.Account{Provider: provider.Trae}, now) != "2026-11-02" {
		t.Fatal("provider timezone changed")
	}
	if WindowOpen(now, 20, 1) || !WindowOpen(now, 20, 0) {
		t.Fatal("claim window boundary changed")
	}
}
