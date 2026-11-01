package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"codearts2api/internal/provider"
)

// ProviderCredentialStore publishes rotated credentials through the existing
// conditional transaction. Implementations must retain the rotated generation
// in memory on a persistence failure; restoring the old token is unsafe.
type ProviderCredentialStore interface {
	Account(string) *Account
	CommitProviderCredentials(id string, expected, updated json.RawMessage) error
}

// CredentialAdapter owns the platform's credential format and refresh protocol.
// CodeArts continues to use its native Manager and immutable Session pointers.
type CredentialAdapter interface {
	Normalize(json.RawMessage) (json.RawMessage, provider.Identity, error)
	Refresh(context.Context, json.RawMessage) (json.RawMessage, error)
}

// ProviderCredentials coordinates opaque provider credentials. It owns the
// per-account refresh lock; the store remains the only credential writer.
type ProviderCredentials struct {
	store        ProviderCredentialStore
	allowRefresh func() bool
	locks        OperationLocks
}

// NewProviderCredentials does no I/O. allowRefresh reads the current setting
// only when refresh is required, preserving hot configuration changes.
func NewProviderCredentials(store ProviderCredentialStore, allowRefresh func() bool) *ProviderCredentials {
	return &ProviderCredentials{store: store, allowRefresh: allowRefresh}
}

// Resolve rereads the latest generation after locking, normalizes once on the
// normal path, and conditionally commits a refresh before releasing the lock.
// Adapter selection remains with the caller; no platform registry is cached.
func (s *ProviderCredentials) Resolve(ctx context.Context, id string, adapter CredentialAdapter) (json.RawMessage, error) {
	release, err := s.locks.Acquire(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()
	latest := s.store.Account(id)
	if latest == nil {
		return nil, ErrCredentialChanged
	}
	raw, identity, err := adapter.Normalize(latest.Credentials)
	if err != nil {
		return nil, err
	}
	if identity.ID != latest.ExternalID || identity.Scope != latest.Scope {
		return nil, &provider.Error{Status: 401, AccountFault: true, Message: "上游凭据身份或地区与账号记录不一致，请重新导入"}
	}
	if time.Unix(identity.ExpiresAt, 0).After(time.Now().Add(5 * time.Minute)) {
		return raw, nil
	}
	if !s.allowRefresh() {
		return nil, &provider.Error{Status: 401, AccountFault: true, Message: "上游凭据已过期，请重新登录或启用续期"}
	}
	updated, err := adapter.Refresh(ctx, raw)
	if err != nil {
		return nil, err
	}
	normalized, fresh, err := adapter.Normalize(updated)
	if err != nil {
		return nil, err
	}
	if fresh.ID != identity.ID || fresh.Scope != identity.Scope {
		return nil, fmt.Errorf("续期账号身份发生变化，拒绝保存")
	}
	if err = s.store.CommitProviderCredentials(id, latest.Credentials, normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}
