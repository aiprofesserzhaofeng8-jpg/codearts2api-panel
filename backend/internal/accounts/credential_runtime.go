package accounts

import (
	"context"
	"sync"
	"time"
)

// CredentialRuntime coordinates immutable native session generations. It does
// not interpret credentials or replace ProviderCredentials' opaque JSON path.
// The platform owns resolution policy, IDE I/O, refresh and generation equality.
// Its zero value is ready to use; do not copy a runtime after first use.
type CredentialRuntime[T CredentialSnapshot] struct {
	Latest  func(string) *Account
	Commit  func(id string, expected, updated T) error
	Persist func(*Account) error
	Logf    func(format string, args ...any)

	mu       sync.Mutex
	liveAt   map[string]time.Time
	liveSess map[string]T
	acctMu   map[string]*sync.Mutex
}

func NewCredentialRuntime[T CredentialSnapshot]() CredentialRuntime[T] {
	return CredentialRuntime[T]{liveAt: map[string]time.Time{}, liveSess: map[string]T{}, acctMu: map[string]*sync.Mutex{}}
}

// Resolve retains the original per-account mutex, including its wait semantics.
// Read the latest account only after locking; hold the lock through native I/O
// and the conditional store commit. Other accounts never wait on this I/O.
func (m *CredentialRuntime[T]) Resolve(ctx context.Context, acct *Account, resolve func(context.Context, *Account) (T, error)) (T, error) {
	lock := m.accountLock(acct.ID)
	lock.Lock()
	defer lock.Unlock()
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if m.Latest != nil {
		latest := m.Latest(acct.ID)
		if latest == nil {
			return zero, ErrCredentialChanged
		}
		*acct = *latest
	}
	return resolve(ctx, acct)
}

// Cached returns the borrowed immutable generation within the platform TTL.
func (m *CredentialRuntime[T]) Cached(id string, ttl time.Duration) T {
	m.mu.Lock()
	defer m.mu.Unlock()
	var zero T
	at, ok := m.liveAt[id]
	if !ok || time.Since(at) > ttl {
		return zero
	}
	return m.liveSess[id]
}

// Remember is called under the account lock after an IDE read or publication.
func (m *CredentialRuntime[T]) Remember(id string, s T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.liveAt == nil {
		m.liveAt = map[string]time.Time{}
	}
	if m.liveSess == nil {
		m.liveSess = map[string]T{}
	}
	m.liveAt[id] = time.Now()
	m.liveSess[id] = s
}

func (m *CredentialRuntime[T]) accountLock(id string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acctMu == nil {
		m.acctMu = map[string]*sync.Mutex{}
	}
	lock, ok := m.acctMu[id]
	if !ok {
		lock = new(sync.Mutex)
		m.acctMu[id] = lock
	}
	return lock
}

// Adopt publishes through the sole store transaction before changing the caller
// snapshot. changed is evaluated by the platform, never by reflection or JSON.
// A failed Commit is returned unchanged; a legacy Persist failure is logged.
func (m *CredentialRuntime[T]) Adopt(acct *Account, expected, updated T, changed bool) error {
	if changed && m.Commit != nil {
		if err := m.Commit(acct.ID, expected, updated); err != nil {
			return err
		}
	}
	acct.Session = updated
	if changed && m.Persist != nil {
		if err := m.Persist(acct); err != nil && m.Logf != nil {
			m.Logf("账号 %s 状态保存失败: %v", acct.DisplayName(), err)
		}
	}
	return nil
}

// Invalidate shares the resolution lock so it cannot race a refresh publication.
func (m *CredentialRuntime[T]) Invalidate(id string) {
	lock := m.accountLock(id)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.liveAt, id)
	delete(m.liveSess, id)
}
