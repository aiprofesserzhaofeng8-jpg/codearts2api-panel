// Package catalog owns model refresh and bounded probe coordination.
// The existing registry remains the single model directory; credentials and
// inference continue through the supplied operations and their original owners.
package catalog

import (
	"context"
	"sync"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog/modelmeta"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

type AccountStore interface {
	Accounts() []*accounts.Account
	Account(string) *accounts.Account
	NextAccount() *accounts.Account
	Settings() config.Settings
}
type Directory interface {
	List(map[string]string) []provider.Model
	Resolve(string, map[string]string) (provider.Model, bool)
	ReplaceProvider(string, string, []provider.Model)
}

type RefreshOperations struct {
	Native   func(context.Context, *accounts.Account) ([]provider.Model, error)
	Provider func(context.Context, *accounts.Account) ([]provider.Model, error)
}
type ProbeOperations struct {
	Native   func(context.Context, *accounts.Account, string) error
	Provider func(context.Context, *accounts.Account, provider.Model, []byte) ([]byte, error)
}

type Runtime struct {
	store          AccountStore
	directory      Directory
	mu             sync.RWMutex
	refreshMu      sync.Mutex
	probeMu        sync.Mutex
	providerErrors map[string]string
	err            string
	loadedAt       time.Time
	// Public numeric references are refreshed under refreshMu only. They do not
	// carry credentials, availability or a second gateway model directory.
	loadReferences func(context.Context) ([]byte, error)
	references     *modelmeta.Registry
	referenceRetry time.Time
}

func NewRuntime(store AccountStore, directory Directory, references ...func(context.Context) ([]byte, error)) *Runtime {
	r := &Runtime{store: store, directory: directory}
	if len(references) > 0 {
		r.loadReferences = references[0]
	}
	return r
}
func (r *Runtime) LoadedAt() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loadedAt
}
func (r *Runtime) Error() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.err
}
func (r *Runtime) ProviderErrors() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]string{}
	for k, v := range r.providerErrors {
		out[k] = v
	}
	return out
}
