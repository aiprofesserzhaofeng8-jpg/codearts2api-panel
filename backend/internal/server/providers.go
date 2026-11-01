package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"codearts2api/internal/accounts"
	"codearts2api/internal/gateway"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/upstream"
)

func (s *Server) providerCredentials(ctx context.Context, acct *accounts.Account) (provider.Adapter, json.RawMessage, error) {
	if s.Providers == nil {
		return nil, nil, fmt.Errorf("上游适配器未初始化")
	}
	adapter, ok := s.Providers.Get(acct.ProviderID())
	if !ok {
		return nil, nil, fmt.Errorf("上游尚未接入")
	}
	raw, err := s.ProviderCredentials.Resolve(ctx, acct.ID, adapter)
	if err != nil {
		return nil, nil, err
	}
	return adapter, raw, nil
}
func (s *Server) providerAttempt(ctx context.Context, acct *accounts.Account, model registry.Model) (upstream.StreamSession, error) {
	startAttempt(ctx, acct, model.ID)
	adapter, raw, err := s.providerCredentials(ctx, acct)
	if err != nil {
		return upstream.StreamSession{}, err
	}
	session, _ := ctx.Value(routingSessionKey{}).(string)
	return upstream.BindStream(adapter, raw, session, requestID(ctx)), nil
}

func (s *Server) chatProvider(ctx context.Context, acct *accounts.Account, model registry.Model, body []byte) ([]byte, error) {
	session, err := s.providerAttempt(ctx, acct, model)
	if err != nil {
		return nil, err
	}
	var result []byte
	err = s.runRequestPolicy(ctx, acct, model, body, func(prepared []byte, observe func()) error {
		var callErr error
		result, callErr = s.Completions.CompleteStream(ctx, session, model.ID, prepared, gateway.Observer{Observe: observe, Usage: captureUsage})
		return callErr
	})
	return result, err
}

func (s *Server) streamProvider(ctx context.Context, acct *accounts.Account, model registry.Model, body []byte, onChunk func([]byte) error) error {
	session, err := s.providerAttempt(ctx, acct, model)
	if err != nil {
		return err
	}
	return s.runRequestPolicy(ctx, acct, model, body, func(prepared []byte, observe func()) error {
		return s.Completions.Stream(ctx, session, model.ID, prepared, gateway.Observer{Observe: observe, Usage: captureUsage}, onChunk)
	})
}
func (s *Server) providerDescriptors() []provider.Descriptor {
	out := []provider.Descriptor{{ID: "codearts", Name: "华为云码道", Available: true, Login: true, Scopes: []string{"cn"}}}
	if s.Providers != nil {
		out = append(out, s.Providers.List()...)
		if _, ok := s.Providers.Get("trae"); ok {
			return out
		}
	}
	return append(out, provider.Descriptor{ID: "trae", Name: "Trae CN / SOLO", Available: false, Reason: "当前服务未加载 Trae 适配器"})
}
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]any{"providers": s.providerDescriptors()})
}
func (s *Server) providerAccountError(ctx context.Context, acct *accounts.Account, model string, err error) bool {
	var p *provider.Error
	if !errors.As(err, &p) {
		return false
	}
	if p.ContentBlocked {
		return true
	}
	if p.AccountFault {
		s.Store.MarkAccountResult(acct.ID, false, p.Error())
		s.Store.CooldownAccount(acct.ID, p.Error(), s.Store.Settings().Pool.BreakerCooldownSec)
	} else if p.Status == 429 || p.Status >= 500 || p.Status == 404 {
		cooldown := s.Store.Settings().Pool.RateCooldownSec
		if acct.ProviderID() == provider.WorkBuddy && workBuddyTransientStatus(p.Status) {
			cooldown = min(cooldown, 5)
		}
		s.Store.MarkModelResult(acct.ID, model, false, p.Error(), cooldown)
	}

	return true
}
func (s *Server) refreshProviderModels(ctx context.Context) map[string]string {
	errs := map[string]string{}
	seen := map[string]bool{}
	for _, a := range s.Store.Accounts() {
		if !a.Enabled || a.ProviderID() == provider.CodeArts {
			continue
		}
		key := a.ProviderID() + "/" + a.Scope
		if seen[key] {
			continue
		}
		adapter, raw, err := s.providerCredentials(ctx, a)
		if err != nil {
			errs[key] = err.Error()
			continue
		}
		models, err := adapter.Models(ctx, raw)
		if err != nil {
			errs[key] = err.Error()
			continue
		}
		s.Reg.ReplaceProvider(a.ProviderID(), a.Scope, models)
		seen[key] = true
		delete(errs, key)
	}
	return errs
}
