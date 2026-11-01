package catalog

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"context"
	"errors"
	"time"
)

// ErrEmptyModelList 表示上游 200 但没返回任何可用模型。
var ErrEmptyModelList = errors.New("上游未返回任何可用模型")

// Refresh serializes updates to the same provider/scope partitions. Failed
// fetches retain their previous lists; the legacy partial-success rule is kept.
func (s *Runtime) Refresh(ctx context.Context, ops RefreshOperations) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	failures := s.refreshProviderModels(ctx, ops)
	err := s.refreshCodeArtsModels(ctx, ops)
	if err != nil && !errors.Is(err, accounts.ErrNoAccount) {
		failures[provider.CodeArts] = err.Error()
	}
	s.mu.Lock()
	s.loadedAt = time.Now()
	s.providerErrors = failures
	s.err = ""
	if err != nil && (!errors.Is(err, accounts.ErrNoAccount) || len(failures) > 0) {
		s.err = err.Error()
	}
	s.mu.Unlock()
	if err != nil {
		for _, a := range s.store.Accounts() {
			if a.Enabled && a.ProviderID() != provider.CodeArts {
				if _, failed := failures[a.ProviderID()+"/"+a.Scope]; !failed {
					return nil
				}
			}
		}
		return err
	}
	return nil
}
func (s *Runtime) refreshCodeArtsModels(ctx context.Context, ops RefreshOperations) error {
	acct := s.store.NextAccount()
	if acct == nil {
		return accounts.ErrNoAccount
	}
	models, err := ops.Native(ctx, acct)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return ErrEmptyModelList
	}
	s.directory.ReplaceProvider(provider.CodeArts, "", models)
	return nil
}

// RefreshIfStale retains the synchronous refresh with its independent 30s
// context. Both successful and failed refreshes retain their original TTL.
func (s *Runtime) RefreshIfStale(maxAge time.Duration, ops RefreshOperations) {
	s.mu.RLock()
	stale := time.Since(s.loadedAt) > maxAge
	s.mu.RUnlock()
	if !stale {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Refresh(ctx, ops); err != nil {
		s.mu.Lock()
		s.err = err.Error()
		s.loadedAt = time.Now()
		s.mu.Unlock()
	}
}

func (s *Runtime) refreshProviderModels(ctx context.Context, ops RefreshOperations) map[string]string {
	errs := map[string]string{}
	seen := map[string]bool{}
	for _, a := range s.store.Accounts() {
		if !a.Enabled || a.ProviderID() == provider.CodeArts {
			continue
		}
		key := a.ProviderID() + "/" + a.Scope
		if seen[key] {
			continue
		}
		models, err := ops.Provider(ctx, a)
		if err != nil {
			errs[key] = err.Error()
			continue
		}
		s.directory.ReplaceProvider(a.ProviderID(), a.Scope, models)
		seen[key] = true
		delete(errs, key)
	}
	return errs
}
