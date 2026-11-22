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
func (s *Runtime) Refresh(ctx context.Context, ops RefreshOperations) (result error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	defer func() {
		if result == nil && ops.Reconcile != nil {
			result = ops.Reconcile()
			if result != nil {
				s.mu.Lock()
				s.err = result.Error()
				s.report.ReconcileError = result.Error()
				s.report.Status = "partial"
				s.mu.Unlock()
			}
		}
	}()
	failures, err, report := s.refreshPartitions(ctx, ops)
	s.mu.Lock()
	s.loadedAt = time.Now()
	s.providerErrors = failures
	s.report = report
	s.err = ""
	if err != nil && (!errors.Is(err, accounts.ErrNoAccount) || len(failures) > 0) {
		s.err = err.Error()
	}
	s.mu.Unlock()
	if report.Status == "success" || report.Status == "partial" {
		return nil
	}
	return err
}

func (s *Runtime) CommitProbePartition(a *accounts.Account, models []provider.Model) error {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	return s.commitPartition(a, a.Scope, models)
}

// Caller holds commitMu across save and publication.
func (s *Runtime) commitPartition(a *accounts.Account, scope string, models []provider.Model) error {
	models = s.withClientReferences(a.ProviderID(), scope, models)
	if store, ok := s.store.(interface {
		SaveCatalogPartition(*accounts.Account, []provider.Model, time.Time) error
	}); ok {
		if err := store.SaveCatalogPartition(a, models, time.Now()); err != nil {
			return err
		}
	}
	s.directory.ReplaceProvider(a.ProviderID(), scope, models)
	if s.revisions == nil {
		s.revisions = make(map[string]uint64)
	}
	s.revisions[a.ProviderID()+"/"+scope]++
	return nil
}
