package store

import (
	"fmt"
	"sync"
	"time"

	"codearts2api/internal/codearts"
)

// PoolSettings governs inference scheduling. Background account maintenance remains independent.
type PoolSettings struct {
	Strategy           string `json:"strategy"`
	MaxInFlight        int    `json:"max_in_flight"`
	StickyTTLSec       int    `json:"sticky_ttl_sec"`
	RateCooldownSec    int    `json:"rate_cooldown_sec"`
	BreakerThreshold   int    `json:"breaker_threshold"`
	BreakerCooldownSec int    `json:"breaker_cooldown_sec"`
}

func DefaultPoolSettings() PoolSettings {
	return PoolSettings{Strategy: "least_inflight", MaxInFlight: 2, StickyTTLSec: 1800,
		RateCooldownSec: 60, BreakerThreshold: 3, BreakerCooldownSec: 300}
}

func (p PoolSettings) Validate() error {
	switch p.Strategy {
	case "round_robin", "least_inflight", "sequential_exhaust":
	default:
		return fmt.Errorf("pool.strategy 必须为 round_robin / least_inflight / sequential_exhaust")
	}
	for _, v := range []struct {
		name            string
		value, min, max int
	}{
		{"max_in_flight", p.MaxInFlight, 1, 32}, {"sticky_ttl_sec", p.StickyTTLSec, 0, 86400},
		{"rate_cooldown_sec", p.RateCooldownSec, 0, 86400}, {"breaker_threshold", p.BreakerThreshold, 1, 100},
		{"breaker_cooldown_sec", p.BreakerCooldownSec, 1, 86400},
	} {
		if v.value < v.min || v.value > v.max {
			return fmt.Errorf("pool.%s 必须在 %d–%d 之间", v.name, v.min, v.max)
		}
	}
	return nil
}

type stickyBinding struct {
	id      string
	expires time.Time
}
type poolRuntime struct {
	inFlight map[string]int
	bindings map[string]stickyBinding
	cursor   int
	gcAt     time.Time
	now      func() time.Time // test clock; always accessed under Store.mu
}

func (s *Store) poolNow() time.Time {
	if s.pool.now != nil {
		return s.pool.now()
	}
	return time.Now()
}

func (s *Store) initPoolLocked() {
	if s.pool.inFlight == nil {
		s.pool.inFlight = make(map[string]int)
	}
	if s.pool.bindings == nil {
		s.pool.bindings = make(map[string]stickyBinding)
	}
}

// AcquireAccount atomically selects and leases one account. Release is idempotent,
// including after deletion, cancellation or a streaming failure. No cooling account
// is used as a fallback. The caller owns the returned account snapshot.
func (s *Store) AcquireAccount(session string, exclude map[string]bool) (*codearts.Account, func()) {
	return s.AcquireAccountForModel(session, "", exclude)
}

func (s *Store) AcquireAccountForModel(session, model string, exclude map[string]bool) (*codearts.Account, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initPoolLocked()
	now, cfg := s.poolNow(), s.st.Settings.Pool
	if !now.Before(s.pool.gcAt) {
		for k, b := range s.pool.bindings {
			if !now.Before(b.expires) {
				delete(s.pool.bindings, k)
			}
		}
		s.pool.gcAt = now.Add(time.Minute)
	}
	eligible := func(a *codearts.Account) bool {
		return a.Enabled && !a.Paused && !exclude[a.ID] && a.CooldownUntil <= now.Unix() && s.st.ModelHealth[a.ID][model].CooldownUntil <= now.Unix() && s.pool.inFlight[a.ID] < cfg.MaxInFlight
	}
	var selected *codearts.Account
	if session != "" && cfg.StickyTTLSec > 0 {
		if b, ok := s.pool.bindings[session]; ok && now.Before(b.expires) {
			for _, a := range s.st.Accounts {
				if a.ID == b.id && eligible(a) {
					selected = a
					break
				}
			}
		}
	}
	n, chosen := len(s.st.Accounts), -1
	if selected == nil {
		for i := 0; i < n; i++ {
			idx := (s.pool.cursor + i) % n
			a := s.st.Accounts[idx]
			if !eligible(a) {
				continue
			}
			if selected == nil || (cfg.Strategy == "least_inflight" && s.pool.inFlight[a.ID] < s.pool.inFlight[selected.ID]) {
				selected, chosen = a, idx
			}
			if cfg.Strategy != "least_inflight" {
				break
			}
		}
	}
	if selected == nil {
		return nil, func() {}
	}
	if chosen >= 0 {
		s.pool.cursor = chosen
		if cfg.Strategy != "sequential_exhaust" {
			s.pool.cursor = (chosen + 1) % n
		}
	}
	id := selected.ID
	s.pool.inFlight[id]++
	selected.LastUsed = now.Unix()
	if session != "" && cfg.StickyTTLSec > 0 {
		// Bounded memory even when clients provide an unbounded number of session IDs.
		if _, ok := s.pool.bindings[session]; !ok && len(s.pool.bindings) >= 4096 {
			var oldest string
			var expiry time.Time
			for k, b := range s.pool.bindings {
				if oldest == "" || b.expires.Before(expiry) {
					oldest, expiry = k, b.expires
				}
			}
			delete(s.pool.bindings, oldest)
		}
		s.pool.bindings[session] = stickyBinding{id: id, expires: now.Add(time.Duration(cfg.StickyTTLSec) * time.Second)}
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.pool.inFlight[id]--
			if s.pool.inFlight[id] <= 0 {
				delete(s.pool.inFlight, id)
			}
		})
	}
	return cloneAccount(selected), release
}

func (s *Store) ForgetSession(key, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.pool.bindings[key]; ok && b.id == id {
		delete(s.pool.bindings, key)
	}
}

func (s *Store) SetAccountPaused(id string, paused bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.st.Accounts {
		if a.ID == id {
			a.Paused = paused
			return true
		}
	}
	return false
}

func (s *Store) CooldownAccount(id, reason string, seconds int) {
	if seconds <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	until := s.poolNow().Add(time.Duration(seconds) * time.Second).Unix()
	for _, a := range s.st.Accounts {
		if a.ID == id && until > a.CooldownUntil {
			a.CooldownUntil, a.CooldownReason = until, reason
			return
		}
	}
}

type PoolAccountState struct {
	State             string `json:"state"`
	InFlight          int    `json:"in_flight"`
	MaxInFlight       int    `json:"max_in_flight"`
	CooldownRemaining int64  `json:"cooldown_remaining_sec"`
}

// PoolSnapshot contains scheduling state only, never credentials or message content.
func (s *Store) PoolSnapshot() map[string]PoolAccountState {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.poolNow().Unix()
	out := make(map[string]PoolAccountState, len(s.st.Accounts))
	for _, a := range s.st.Accounts {
		v := PoolAccountState{State: "ready", InFlight: s.pool.inFlight[a.ID], MaxInFlight: s.st.Settings.Pool.MaxInFlight, CooldownRemaining: max(0, a.CooldownUntil-now)}
		switch {
		case !a.Enabled:
			v.State = "disabled"
		case a.Paused:
			v.State = "paused"
		case v.CooldownRemaining > 0:
			v.State = "cooling"
		case v.InFlight >= v.MaxInFlight:
			v.State = "busy"
		}
		out[a.ID] = v
	}
	return out
}
