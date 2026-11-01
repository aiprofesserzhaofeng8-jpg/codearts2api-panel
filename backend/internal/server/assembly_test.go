package server

import (
	"log"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
	"codearts2api/internal/trae"
	"codearts2api/internal/websearch"
	"codearts2api/internal/workbuddy"
)

// Tests in package server cannot import app (it consumes server). Supply
// explicit fixture dependencies; production assembly lives only in app.Build.
func newConfiguredServer(st *store.Store) *Server {
	up := codearts.NewClient()
	adapters := provider.NewRegistry()
	for _, adapter := range []provider.Adapter{workbuddy.New(), trae.New()} {
		if err := adapters.Register(adapter); err != nil {
			panic(err)
		}
	}
	credentials := codearts.NewManager(up)
	credentials.Latest = st.Account
	credentials.Commit = st.CommitAccountSession
	credentials.Logf = log.Printf
	return New(st, Dependencies{
		Up: up, Cred: credentials, Logins: codearts.NewLoginManager(up),
		Providers: adapters, Reg: registry.New(), Search: websearch.New(),
	})
}
