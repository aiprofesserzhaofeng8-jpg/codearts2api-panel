package catalog

import (
	"context"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

func TestRefreshTimeoutKeepsLastSuccessfulCatalogAndRecovers(t *testing.T) {
	for _, warm := range []bool{false, true} {
		name := "cold-start"
		if warm {
			name = "existing-catalog"
		}
		t.Run(name, func(t *testing.T) {
			r, _, directory := runtimeFixture(t,
				&accounts.Account{ID: "healthy", Provider: provider.WorkBuddy, Scope: "cn", Enabled: true},
				&accounts.Account{ID: "slow", Provider: provider.Trae, Scope: "cn", Enabled: true},
			)
			healthy := provider.ModelID(provider.WorkBuddy, "cn", "healthy-model")
			previous := provider.ModelID(provider.Trae, "cn", "previous-model")
			fresh := provider.ModelID(provider.Trae, "cn", "fresh-model")
			if warm {
				directory.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "previous-model"}})
			}

			ops := RefreshOperations{
				Provider: func(ctx context.Context, a *accounts.Account) ([]provider.Model, error) {
					if a.ID == "slow" {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return []provider.Model{{ID: "healthy-model"}}, nil
				},
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			if err := r.Refresh(ctx, ops); err != nil {
				t.Fatalf("healthy partition should allow partial success: %v", err)
			}
			if _, ok := directory.ResolveRaw(healthy); !ok {
				t.Fatal("healthy partition was lost")
			}
			if _, ok := directory.ResolveRaw(previous); ok != warm {
				t.Fatalf("last successful partition present=%t, want %t", ok, warm)
			}
			failures := r.ProviderErrors()
			if len(failures) != 1 || !strings.Contains(failures[provider.Trae+"/cn"], context.DeadlineExceeded.Error()) {
				t.Fatalf("expected a single partition timeout, got %v", failures)
			}

			ops.Provider = func(_ context.Context, a *accounts.Account) ([]provider.Model, error) {
				if a.ID == "slow" {
					return []provider.Model{{ID: "fresh-model"}}, nil
				}
				return []provider.Model{{ID: "healthy-model"}}, nil
			}
			if err := r.Refresh(t.Context(), ops); err != nil || r.Error() != "" || len(r.ProviderErrors()) != 0 {
				t.Fatalf("successful retry did not clear the timeout: %v, %q, %v", err, r.Error(), r.ProviderErrors())
			}
			if _, ok := directory.ResolveRaw(fresh); !ok {
				t.Fatal("recovered partition was not published")
			}
			if _, ok := directory.ResolveRaw(previous); ok {
				t.Fatal("old models remained after a successful refresh")
			}
		})
	}
}
