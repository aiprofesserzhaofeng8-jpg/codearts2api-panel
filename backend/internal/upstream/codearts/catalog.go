package codearts

import (
	"codearts2api/internal/accounts"
	legacy "codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"context"
)

func (a *Adapter) Models(ctx context.Context, manager *legacy.Manager, account *accounts.Account, options legacy.ResolveOptions) ([]provider.Model, error) {
	session, err := manager.Session(ctx, account, options)
	if err != nil {
		return nil, err
	}
	list, err := a.client.FetchBuiltinModels(ctx, session)
	if err != nil {
		return nil, err
	}
	return registry.FromBuiltin(list), nil
}
func (a *Adapter) Probe(ctx context.Context, manager *legacy.Manager, account *accounts.Account, options legacy.ResolveOptions, model string) error {
	session, err := manager.Session(ctx, account, options)
	if err != nil {
		return err
	}
	return a.client.Probe(ctx, session, model)
}
