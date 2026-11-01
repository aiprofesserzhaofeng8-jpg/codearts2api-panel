package server

import (
	"log"
	"time"

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
	logins := codearts.NewLoginManager(up)
	models := registry.New()
	return New(st, Dependencies{
		CodeArts: codeartsup.New(up), Completions: &gateway.Completions{MarkStreamOnly: models.MarkStreamOnly},
		Up: up, Cred: credentials, Logins: logins,
		Providers: adapters, Reg: models, Search: websearch.New(),
		AccountManagement: accounts.NewManagement(st, &codeartsup.AccountManagement{Manager: credentials, Options: func() codearts.ResolveOptions {
			settings := st.Settings()
			options := codearts.DefaultResolveOptions()
			options.LiveTTL = time.Duration(settings.LiveTTLSeconds) * time.Second
			options.AllowRefresh, options.WriteBack = settings.AutoRefresh, settings.WriteBackToIDE
			return options
		}}, credentials.Invalidate, log.Printf),
		BrowserLogins:       accounts.NewBrowserLogins(st, codeartsup.NewBrowserLogin(logins, up), codearts.RandomHex, func() int { return st.Settings().LoginPort }, credentials.Invalidate),
		ProviderLogins:      accounts.NewProviderLogins(st, codearts.RandomHex),
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

func attachAccountManagement(s *Server) {
	s.AccountManagement = accounts.NewManagement(s.Store, &codeartsup.AccountManagement{Manager: s.Cred, Options: s.resolveOptions}, s.Cred.Invalidate, log.Printf)
}
