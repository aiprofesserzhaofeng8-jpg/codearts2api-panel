package server

import (
	"context"
	"errors"
	"log"
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog"
	"codearts2api/internal/codearts"
	"codearts2api/internal/config"
	"codearts2api/internal/gateway"
	"codearts2api/internal/jobs"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/storage"
	"codearts2api/internal/trae"
	codeartsup "codearts2api/internal/upstream/codearts"
	"codearts2api/internal/usage"
	"codearts2api/internal/websearch"
	"codearts2api/internal/workbuddy"
)

// Tests in package server cannot import app (it consumes server). Supply
// explicit fixture dependencies; production assembly lives only in app.Build.
func newConfiguredServer(st *storage.Store) *Server {
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
	s := New(st, Dependencies{
		Admission: gateway.NewAdmission(st),
		CodeArts:  codeartsup.New(up), Completions: &gateway.Completions{MarkStreamOnly: models.MarkStreamOnly},
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
	attachAccessManagement(s)
	return s
}

func attachAccessManagement(s *Server) {
	attachModelPolicies(s)
	attachRewards(s)
	if s.Catalog == nil {
		s.Catalog = catalog.NewRuntime(s.Store, s.Reg)
	}
	s.Configuration = config.NewManagement(s.Store, func(id string) (config.ModelRef, bool) {
		model, ok := s.Reg.Resolve(id, s.Store.Settings().ModelAliases)
		return config.ModelRef{ID: model.ID, Provider: modelProvider(model), Scope: model.Scope}, ok
	}, log.Printf)
	s.AccessManagement = access.NewManagement(s.Store, access.ModelResolver{
		Raw: func(id string) (string, bool) {
			model, ok := s.Reg.ResolveRaw(id)
			return model.ID, ok
		},
		Aliased: func(id string) (string, bool) {
			model, ok := s.Reg.Resolve(id, s.Store.Settings().ModelAliases)
			return model.ID, ok
		},
	}, codearts.Mask, log.Printf)
}

// Narrow fixture constructors intentionally omit unrelated app services.
func attachChatExecution(s *Server) *Server {
	s.Admission = gateway.NewAdmission(s.Store)
	attachModelPolicies(s)
	attachRewards(s)
	if s.Catalog == nil && s.Reg != nil {
		s.Catalog = catalog.NewRuntime(s.Store, s.Reg)
	}
	s.CodeArts = codeartsup.New(s.Up)
	s.Completions = &gateway.Completions{}
	s.ProviderCredentials = accounts.NewProviderCredentials(s.Store, func() bool { return s.Store.Settings().AutoRefresh })
	if s.Reg != nil {
		s.Completions.MarkStreamOnly = s.Reg.MarkStreamOnly
	}
	return s
}

func attachAccountManagement(s *Server) {
	s.AccountProbes = accounts.NewProbes(s.Store, s.Reg, log.Printf)
	s.AccountManagement = accounts.NewManagement(s.Store, &codeartsup.AccountManagement{Manager: s.Cred, Options: s.resolveOptions}, s.Cred.Invalidate, log.Printf)
}

// A failed isolated refresh warms metadata without mutating fixture catalogs or
// resolving credentials. Failed refreshes retain the same TTL as successful ones.
func markModelsFresh(s *Server) {
	fail := func(context.Context, *accounts.Account) ([]provider.Model, error) {
		return nil, errors.New("isolated fixture refresh")
	}
	_ = s.Catalog.Refresh(context.Background(), catalog.RefreshOperations{Native: fail, Provider: fail})
}

func attachRewards(s *Server) {
	if s.AdminSessions == nil {
		s.AdminSessions = &access.AdminSessions{}
	}
	if s.Pricing == nil {
		s.Pricing = usage.NewManagement(s.Store)
	}
	if s.Welfare == nil {
		s.Welfare = jobs.NewWelfare(s.Store)
	}
	if s.Credits == nil {
		s.Credits = &accounts.Credits{}
	}
	if s.Growth == nil {
		s.Growth = jobs.NewGrowth(s.Store.Path()+".growth.json", s.Credits.Invalidate)
	}
	if s.Rewards == nil {
		s.Rewards = jobs.NewRewards(s.Store, s.Credits.Invalidate, log.Printf)
	}
}

// Compatibility helpers for the original integration fixtures.
func (s *Server) growthExecutionPath() string { return s.Store.Path() + ".growth.json" }
func growthTask(tasks []provider.GrowthTask, code string) *provider.GrowthTask {
	return jobs.GrowthTask(tasks, code)
}

func attachModelPolicies(s *Server) {
	if s.Routing == nil && s.Reg != nil {
		s.Routing = catalog.NewRouting(s.Store, s.Reg)
	}
	if s.ModelPolicies == nil && s.Reg != nil {
		s.ModelPolicies = config.NewModelPolicies(s.Store, s.Reg)
	}
}
