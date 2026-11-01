package server

import (
	"log"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
	"codearts2api/internal/gateway"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
	"codearts2api/internal/trae"
	codeartsup "codearts2api/internal/upstream/codearts"
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
	models := registry.New()
	return New(st, Dependencies{
		CodeArts: codeartsup.New(up), Completions: &gateway.Completions{MarkStreamOnly: models.MarkStreamOnly},
		Up: up, Cred: credentials, Logins: codearts.NewLoginManager(up),
		Providers: adapters, Reg: models, Search: websearch.New(),
		ProviderCredentials: accounts.NewProviderCredentials(st, func() bool { return st.Settings().AutoRefresh }),
	})
}

// Narrow fixture constructors intentionally omit unrelated app services.
func attachChatExecution(s *Server) *Server {
	s.CodeArts = codeartsup.New(s.Up)
	s.Completions = &gateway.Completions{}
	s.ProviderCredentials = accounts.NewProviderCredentials(s.Store, func() bool { return s.Store.Settings().AutoRefresh })
	if s.Reg != nil {
		s.Completions.MarkStreamOnly = s.Reg.MarkStreamOnly
	}
	return s
}
