// Package app assembles platform services and their persistence callbacks.
// Construction performs no upstream I/O and starts no background tasks.
package app

import (
	"fmt"
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
	"codearts2api/internal/server"
	"codearts2api/internal/store"
	"codearts2api/internal/trae"
	codeartsup "codearts2api/internal/upstream/codearts"
	"codearts2api/internal/usage"
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
	logins := codearts.NewLoginManager(up)
	models := registry.New()
	credits := &accounts.Credits{}
	return server.New(st, server.Dependencies{
		AdminSessions: &access.AdminSessions{}, Pricing: usage.NewManagement(st),
		Welfare: jobs.NewWelfare(st),
		Growth:  jobs.NewGrowth(st.Path()+".growth.json", credits.Invalidate),
		Credits: credits, Rewards: jobs.NewRewards(st, credits.Invalidate, log.Printf),
		Catalog:       catalog.NewRuntime(st, models),
		AccountProbes: accounts.NewProbes(st, models, log.Printf),
		ModelPolicies: config.NewModelPolicies(st, models),
		Routing:       catalog.NewRouting(st, models),
		Configuration: config.NewManagement(st, func(id string) (config.ModelRef, bool) {
			model, ok := models.Resolve(id, st.Settings().ModelAliases)
			platform := model.Provider
			if platform == "" {
				platform = provider.CodeArts
			}
			return config.ModelRef{ID: model.ID, Provider: platform, Scope: model.Scope}, ok
		}, log.Printf),
		AccessManagement: access.NewManagement(st, access.ModelResolver{
			Raw: func(id string) (string, bool) {
				model, ok := models.ResolveRaw(id)
				return model.ID, ok
			},
			Aliased: func(id string) (string, bool) {
				model, ok := models.Resolve(id, st.Settings().ModelAliases)
				return model.ID, ok
			},
		}, codearts.Mask, log.Printf),
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
	}), nil
}
