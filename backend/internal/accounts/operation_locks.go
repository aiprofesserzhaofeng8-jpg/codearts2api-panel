package accounts

import (
	"context"
	"sync"
)

// OperationLocks serializes one kind of operation per account. Its zero value
// is ready to use. Refresh, rewards and growth tasks use separate instances;
// none of these locks replaces or shares the scheduler's admission lock.
// Entries live for the service lifetime, as they did in the HTTP runtime.
type OperationLocks struct {
	mu    sync.Mutex
	locks map[string]chan struct{}
}

// Acquire waits interruptibly. The caller must release exactly once, after any
// network operation and credential commit protected by this account lock.
func (p *OperationLocks) Acquire(ctx context.Context, id string) (func(), error) {
	p.mu.Lock()
	if p.locks == nil {
		p.locks = map[string]chan struct{}{}
	}
	ch := p.locks[id]
	if ch == nil {
		ch = make(chan struct{}, 1)
		p.locks[id] = ch
	}
	p.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
