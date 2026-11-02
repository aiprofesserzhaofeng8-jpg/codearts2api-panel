package storage

import (
	"fmt"
	"sync"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/scheduler"
)

// PoolSettings retains the original scheduling configuration type.
type PoolSettings = config.PoolSettings

func DefaultPoolSettings() PoolSettings { return config.DefaultPoolSettings() }

func accountMaxInFlight(a *accounts.Account, defaults PoolSettings) int {
	return scheduler.MaxInFlight(a.MaxInFlight, defaults.MaxInFlight)
}

type stickyBinding struct {
	model   string
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
func (s *Store) AcquireAccount(session string, exclude map[string]bool) (*accounts.Account, func()) {
	return s.AcquireAccountForModel(session, "", exclude)
}

func (s *Store) AcquireAccountForModel(session, model string, exclude map[string]bool) (*accounts.Account, func()) {
	a, release, _ := s.AcquireAccountForKey(session, model, exclude, "")
	return a, release
}

func (s *Store) AcquireAccountForKey(session, model string, exclude map[string]bool, key string) (*accounts.Account, func(), error) {
	a, _, release, err := s.acquireAccountForModels(session, []string{model}, exclude, key, "")
	return a, release, err
}

// AcquireMappedAccount selects the concrete model and leases its account in the
// same critical section. Current group membership and mappings are rechecked.
func (s *Store) AcquireMappedAccount(session, public string, models []string, exclude map[string]bool, key string) (*accounts.Account, string, func(), error) {
	return s.acquireAccountForModels(session, models, exclude, key, public)
}
func (s *Store) acquireAccountForModels(session string, models []string, exclude map[string]bool, key, public string) (*accounts.Account, string, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	access, err := s.accessScopeLocked(key)
	if err != nil {
		return nil, "", func() {}, err
	}
	allowedSources, mapped := access.Mapping(public)
	if public != "" && (!mapped || !access.Callable(public)) {
		return nil, "", func() {}, fmt.Errorf("分组统一模型已删除")
	}
	type target struct{ id, platform, scope string }
	var smallTargets [4]target
	targets := smallTargets[:0]
	if len(models) > cap(targets) {
		targets = make([]target, 0, len(models))
	}
	for _, model := range models {
		if !access.AllowsModel(model) {
			continue
		}
		if public != "" {
			allowed := false
			for _, id := range allowedSources {
				if id == model {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		p, scope, _ := provider.ParseModel(model)
		targets = append(targets, target{model, p, scope})
	}
	if len(targets) == 0 {
		return nil, "", func() {}, fmt.Errorf("分组不允许调用此模型")
	}
	targetFor := func(a *accounts.Account) string {
		for _, t := range targets {
			if a.ProviderID() == t.platform && (t.platform == provider.CodeArts || a.Scope == t.scope) {
				return t.id
			}
		}
		return ""
	}
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
	eligible := func(a *accounts.Account) bool {
		if access.Restricted && !access.AccountIDs[a.ID] {
			return false
		}
		model := targetFor(a)
		if model == "" && !(public == "" && len(models) == 1 && models[0] == "" && a.ProviderID() == provider.CodeArts) {
			return false
		}
		return a.Enabled && !a.Paused && !exclude[a.ID] && a.CooldownUntil <= now.Unix() && s.st.ModelHealth[a.ID][model].CooldownUntil <= now.Unix() && s.pool.inFlight[a.ID] < accountMaxInFlight(a, cfg)
	}
	var selected *accounts.Account
	if session != "" && cfg.StickyTTLSec > 0 {
		if b, ok := s.pool.bindings[session]; ok && now.Before(b.expires) {
			for _, a := range s.st.Accounts {
				if a.ID == b.id && eligible(a) && (public == "" || b.model == targetFor(a)) {
					selected = a
					break
				}
			}
		}
	}
	if selected == nil {
		chosen, cursor := scheduler.Select(len(s.st.Accounts), s.pool.cursor, cfg.Strategy, func(idx int) (bool, int) {
			a := s.st.Accounts[idx]
			return eligible(a), s.pool.inFlight[a.ID]
		})
		if chosen < 0 {
			return nil, "", func() {}, nil
		}
		selected = s.st.Accounts[chosen]
		s.pool.cursor = cursor
	}
	selectedModel := targetFor(selected)
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
		s.pool.bindings[session] = stickyBinding{model: selectedModel, id: id, expires: now.Add(time.Duration(cfg.StickyTTLSec) * time.Second)}
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
	return cloneAccount(selected), selectedModel, release, nil
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
		v := PoolAccountState{State: "ready", InFlight: s.pool.inFlight[a.ID], MaxInFlight: accountMaxInFlight(a, s.st.Settings.Pool), CooldownRemaining: max(0, a.CooldownUntil-now)}
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
