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
	type creditRow struct {
		idx int
		row map[string]any
	}
	results := make([]map[string]any, len(targets))
	ch := make(chan creditRow, len(targets))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, a := range targets {
		wg.Add(1)
		go func(idx int, a *Account) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			row := map[string]any{"id": a.ID, "name": a.DisplayName()}
			info, err := s.For(ctx, a, fresh, ops)
			if err != nil {
				row["ok"] = false
				row["error"] = err.Error()
			} else {
				row["ok"] = true
				row["credit"] = info
			}
			// 只往 channel 写，切片由主 goroutine 汇总，避免并发写。
			ch <- creditRow{idx: idx, row: row}
		}(i, a)
	}
	wg.Wait()
	close(ch)
	for c := range ch {
		results[c.idx] = c.row
	}

	return results
}
