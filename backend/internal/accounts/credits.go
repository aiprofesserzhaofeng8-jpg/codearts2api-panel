package accounts

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"sync"
	"time"
)

const creditCacheTTL = 60 * time.Second

type creditCacheEntry struct {
	info *CreditInfo
	err  error
	at   time.Time
}

// Credits owns only the original short query cache. It never updates inference
// account health or interprets native credentials.
type Credits struct {
	creditMu    sync.Mutex
	creditCache map[string]creditCacheEntry
}
type CreditOperations struct {
	Native   func(context.Context, *Account) (*CreditInfo, error)
	Provider func(context.Context, *Account) (provider.Adapter, json.RawMessage, error)
}

func (s *Credits) For(ctx context.Context, a *Account, fresh bool, ops CreditOperations) (*CreditInfo, error) {
	if a.ProviderID() != provider.CodeArts {
		if !fresh {
			if info, err, ok := s.Cached(a.ID); ok {
				return info, err
			}
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		adapter, raw, err := ops.Provider(ctx, a)
		if err != nil {
			return nil, err
		}
		data, err := adapter.Credit(ctx, raw)
		if err != nil {
			s.Store(a.ID, nil, err)
			return nil, err
		}
		var info CreditInfo
		if err = json.Unmarshal(data, &info); err != nil {
			return nil, err
		}
		s.Store(a.ID, &info, nil)
		return &info, nil
	}

	if !fresh {
		if info, err, ok := s.Cached(a.ID); ok {
			return info, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := ops.Native(ctx, a)
	s.Store(a.ID, info, err)
	return info, err
}

func (s *Credits) Cached(id string) (*CreditInfo, error, bool) {
	s.creditMu.Lock()
	defer s.creditMu.Unlock()
	e, ok := s.creditCache[id]
	if !ok || time.Since(e.at) > creditCacheTTL {
		return nil, nil, false
	}
	return e.info, e.err, true
}

// Snapshot projects the existing query cache even when stale. It never performs
// a quota query or refresh and never turns a missing observation into zero.
func (s *Credits) Snapshot(id string) (*CreditInfo, time.Time, bool, bool) {
	s.creditMu.Lock()
	defer s.creditMu.Unlock()
	e, ok := s.creditCache[id]
	return e.info, e.at.UTC(), !e.at.IsZero() && time.Since(e.at) > creditCacheTTL, ok && e.err == nil && e.info != nil
}

func (s *Credits) Store(id string, info *CreditInfo, err error) {
	s.creditMu.Lock()
	defer s.creditMu.Unlock()
	if s.creditCache == nil {
		s.creditCache = map[string]creditCacheEntry{}
	}
	s.creditCache[id] = creditCacheEntry{info: info, err: err, at: time.Now()}
}

func (s *Credits) Invalidate(id string) {
	s.creditMu.Lock()
	delete(s.creditCache, id)
	s.creditMu.Unlock()
}

// Rows preserves the original four-account query concurrency and result order.
func (s *Credits) Rows(ctx context.Context, targets []*Account, fresh bool, ops CreditOperations) []map[string]any {
	results := make([]map[string]any, len(targets))
	var wg sync.WaitGroup
	// A fixed worker count bounds goroutines as well as upstream requests.
	for worker := 0; worker < min(4, len(targets)); worker++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := offset; i < len(targets); i += 4 {
				a := targets[i]
				row := map[string]any{"id": a.ID, "name": a.DisplayName()}
				var info *CreditInfo
				err := ctx.Err()
				if err == nil {
					info, err = s.For(ctx, a, fresh, ops)
				}
				if err != nil {
					row["ok"] = false
					row["error"] = err.Error()
				} else {
					row["ok"] = true
					row["credit"] = info
				}
				results[i] = row
			}
		}(worker)
	}
	wg.Wait()
	return results
}
