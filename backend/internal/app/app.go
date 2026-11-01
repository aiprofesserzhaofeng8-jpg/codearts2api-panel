// Package app assembles platform services and their persistence callbacks.
// Construction performs no upstream I/O and starts no background tasks.
package app

import (
	"fmt"
	"log"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/server"
	"codearts2api/internal/store"
	"codearts2api/internal/trae"
	"codearts2api/internal/websearch"
	"codearts2api/internal/workbuddy"
)

func Build(st *store.Store) (*server.Server, error) {
	if st == nil {
		return nil, fmt.Errorf("application store is required")
	}
	up := codearts.NewClient()
	adapters := provider.NewRegistry()
	for _, adapter := range []provider.Adapter{workbuddy.New(), trae.New()} {
		if err := adapters.Register(adapter); err != nil {
			return nil, fmt.Errorf("register %s: %w", adapter.Describe().ID, err)
		}
	}
	credentials := codearts.NewManager(up)
	// Resolved accounts are snapshots. Publish refreshes through the existing
	// generation-checked store transaction so stale logins cannot overwrite them.
	credentials.Latest = st.Account
	credentials.Commit = st.CommitAccountSession
	credentials.Logf = log.Printf
	return server.New(st, server.Dependencies{
		Up: up, Cred: credentials, Logins: codearts.NewLoginManager(up),
		Providers: adapters, Reg: registry.New(), Search: websearch.New(),
	}), nil
}
