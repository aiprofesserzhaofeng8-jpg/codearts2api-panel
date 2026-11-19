package catalog

import (
	"context"
	"errors"
	"sort"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

// AccountModelList is an observation, not a routing allowlist.
type AccountModelList accounts.ModelList

type ObservedModel = accounts.ModelObservation

// DiscoverAccountModels borrows the same credential-aware operation as Refresh,
// but never tries another account or publishes to the shared directory.
func (r *Runtime) DiscoverAccountModels(ctx context.Context, id string, fetch func(context.Context, *accounts.Account) ([]provider.Model, error)) (*AccountModelList, error) {
	a := r.store.Account(id)
	if a == nil || !a.Enabled {
		return nil, accounts.ErrNoAccount
	}
	rows, err := fetch(ctx, a)
	if err != nil {
		return nil, err
	}
	result := &AccountModelList{AccountID: a.ID, Provider: a.ProviderID(), Scope: a.Scope, ObservedAt: time.Now().UTC(), Models: make([]ObservedModel, 0, len(rows))}
	result.Kind = "live"
	switch {
	case a.ProviderID() == provider.Vertex:
		result.Kind = "configured"
	case a.ProviderID() == provider.TeleAgent || a.ProviderID() == "gemini" && a.Scope == "cli":
		result.Kind = "reference"
	case a.ProviderID() == provider.TypeSafe:
		result.Kind = "decision"
	}
	for _, m := range rows {
		if m.CatalogKind != "" {
			result.Kind = m.CatalogKind
		}
		id := m.UpstreamID
		if id == "" {
			id = m.ID
		}
		result.Models = append(result.Models, ObservedModel{ID: id, Name: m.Name, Context: m.Context, MaxOutput: m.MaxOutput, Credit: m.Credit, ImageInput: m.ImageInput, ToolCalling: m.ToolCalling, Protocols: m.SupportedProtocols, NonStreaming: !m.StreamOnly})
	}
	return result, nil
}

type AccountModelDiff = accounts.ModelDelta

// Retained compares membership by upstream ID, not mutable display metadata.
func (next AccountModelList) Diff(previous *AccountModelList) (AccountModelDiff, error) {
	var d AccountModelDiff
	known := map[string]bool{}
	if previous != nil {
		if previous.AccountID != next.AccountID || previous.Provider != next.Provider || previous.Scope != next.Scope {
			return d, errors.New("不能跨账号或区域比较模型清单")
		}
		for _, m := range previous.Models {
			known[m.ID] = true
		}
	}
	for _, m := range next.Models {
		if known[m.ID] {
			d.Retained = append(d.Retained, m.ID)
			delete(known, m.ID)
		} else {
			d.Added = append(d.Added, m.ID)
		}
	}
	for id := range known {
		d.Missing = append(d.Missing, id)
	}
	sort.Strings(d.Added)
	sort.Strings(d.Missing)
	sort.Strings(d.Retained)
	return d, nil
}
