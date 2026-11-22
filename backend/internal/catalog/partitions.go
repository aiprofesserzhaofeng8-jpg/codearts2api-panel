package catalog

import (
	"context"
	"errors"
	"sync"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

type partitionResult struct {
	key      string
	scope    string
	account  *accounts.Account
	models   []provider.Model
	revision uint64
	err      error
}

func (s *Runtime) refreshPartitions(ctx context.Context, ops RefreshOperations) (map[string]string, error, RefreshReport) {
	groups := map[string][]*accounts.Account{}
	var order []string
	for _, a := range s.store.Accounts() {
		if !a.Enabled || a.ProviderID() == provider.CodeArts {
			continue
		}
		key := a.ProviderID() + "/" + a.Scope
		if len(groups[key]) == 0 {
			order = append(order, key)
		}
		groups[key] = append(groups[key], a)
	}
	nativeErr := accounts.ErrNoAccount
	if a := s.store.NextAccount(); a != nil {
		order = append(order, provider.CodeArts)
		groups[provider.CodeArts] = []*accounts.Account{a}
	}
	jobs := make(chan string, len(order))
	for _, key := range order {
		jobs <- key
	}
	close(jobs)
	results := make(chan partitionResult, len(order))
	var wg sync.WaitGroup
	for range min(4, len(order)) {
		wg.Go(func() {
			for key := range jobs {
				r := partitionResult{key: key}
				if ctx.Err() != nil {
					r.err = ctx.Err()
					results <- r
					continue
				}
				part, cancel := context.WithTimeout(ctx, 30*time.Second)
				for _, a := range groups[key] {
					r.account = a
					r.scope = a.Scope
					if key == provider.CodeArts {
						r.scope = ""
						r.models, r.err = ops.Native(part, a)
						if r.err == nil && len(r.models) == 0 {
							r.err = ErrEmptyModelList
						}
					} else {
						r.models, r.err = ops.Provider(part, a)
					}
					if part.Err() != nil {
						r.err = part.Err()
						break
					}
					if r.err == nil {
						break
					}
				}
				cancel()
				results <- r
			}
		})
	}
	go func() { wg.Wait(); close(results) }()
	failures := map[string]string{}
	report := RefreshReport{Partitions: map[string]PartitionStatus{}}
	var committed []partitionResult
	for r := range results {
		if r.err == nil {
			s.commitMu.Lock()
			r.err = s.commitPartition(r.account, r.scope, r.models)
			r.revision = s.revisions[r.account.ProviderID()+"/"+r.scope]
			s.commitMu.Unlock()
		}
		status := PartitionStatus{Status: "success", Count: len(r.models)}
		source := groups[r.key][0]
		scope := source.Scope
		if r.key == provider.CodeArts {
			scope = ""
		}
		if st, ok := s.store.(interface {
			CatalogPartitionStatus(string, string) (int, *time.Time)
		}); ok {
			status.Count, status.LastSuccess = st.CatalogPartitionStatus(source.ProviderID(), scope)
		}
		if r.err != nil {
			status.Status = "failed"
			status.Error = r.err.Error()
			if r.account == nil {
				status.Status = "not_started"
			} else if errors.Is(r.err, context.DeadlineExceeded) {
				status.Status = "timeout"
			}
		}
		report.Partitions[r.key] = status
		if r.key == provider.CodeArts {
			nativeErr = r.err
		}
		if r.err != nil {
			failures[r.key] = r.err.Error()
		} else {
			committed = append(committed, r)
		}
	}
	if ctx.Err() == nil {
		previous := s.references
		s.refreshReferences(ctx)
		if s.references != previous {
			for _, r := range committed {
				s.commitMu.Lock()
				if s.revisions[r.account.ProviderID()+"/"+r.scope] != r.revision {
					s.commitMu.Unlock()
					continue
				}
				err := s.commitPartition(r.account, r.scope, r.models)
				s.commitMu.Unlock()
				if err != nil {
					failures[r.key] = err.Error()
					status := report.Partitions[r.key]
					status.Error = err.Error()
					status.Status = "metadata_failed"
					report.Partitions[r.key] = status
				}
			}
		}
	}
	switch {
	case len(order) == 0:
		report.Status = "empty"
	case len(committed) == 0:
		report.Status = "failed"
	case len(failures) > 0:
		report.Status = "partial"
	default:
		report.Status = "success"
	}
	return failures, nativeErr, report
}
