package server

import (
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type providerRuntime struct {
	mu    sync.Mutex
	locks map[string]chan struct{}
}

func (p *providerRuntime) lock(ctx context.Context, id string) (func(), error) {
	p.mu.Lock()
	if p.locks == nil {
		p.locks = map[string]chan struct{}{}
	}
	ch := p.locks[id]
	if ch == nil {
		ch = make(chan struct{}, 1)
		p.locks[id] = ch
	}
	p.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *Server) providerCredentials(ctx context.Context, acct *codearts.Account) (provider.Adapter, json.RawMessage, error) {
	if s.Providers == nil {
		return nil, nil, fmt.Errorf("上游适配器未初始化")
	}
	adapter, ok := s.Providers.Get(acct.ProviderID())
	if !ok {
		return nil, nil, fmt.Errorf("上游尚未接入")
	}
	release, err := s.providerRuntime.lock(ctx, acct.ID)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	latest := s.Store.Account(acct.ID)
	if latest == nil {
		return nil, nil, codearts.ErrSessionChanged
	}
	raw, identity, err := adapter.Normalize(latest.Credentials)
	if err != nil {
		return nil, nil, err
	}
	if identity.ID != latest.ExternalID || identity.Scope != latest.Scope {
		return nil, nil, &provider.Error{Status: 401, AccountFault: true, Message: "上游凭据身份或地区与账号记录不一致，请重新导入"}
	}
	if time.Unix(identity.ExpiresAt, 0).After(time.Now().Add(5 * time.Minute)) {
		return adapter, raw, nil
	}
	if !s.Store.Settings().AutoRefresh {
		return nil, nil, &provider.Error{Status: 401, AccountFault: true, Message: "上游凭据已过期，请重新登录或启用续期"}
	}
	updated, err := adapter.Refresh(ctx, raw)
	if err != nil {
		return nil, nil, err
	}
	normalized, fresh, err := adapter.Normalize(updated)
	if err != nil {
		return nil, nil, err
	}
	if fresh.ID != identity.ID || fresh.Scope != identity.Scope {
		return nil, nil, fmt.Errorf("续期账号身份发生变化，拒绝保存")
	}
	if err = s.Store.CommitProviderCredentials(acct.ID, latest.Credentials, normalized); err != nil {
		return nil, nil, err
	}
	return adapter, normalized, nil
}
func (s *Server) streamProvider(ctx context.Context, acct *codearts.Account, model registry.Model, body []byte, onChunk func([]byte) error) error {
	startAttempt(ctx, acct, model.ID)
	adapter, raw, err := s.providerCredentials(ctx, acct)
	if err != nil {
		return err
	}
	session, _ := ctx.Value(routingSessionKey{}).(string)
	_, _, upstream := provider.ParseModel(model.ID)
	return adapter.Stream(ctx, raw, provider.Request{Model: upstream, Body: body, SessionID: session, RequestID: requestID(ctx)}, func(b []byte) error { captureUsage(ctx, b); return onChunk(b) })
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
func (s *Server) providerAccountError(ctx context.Context, acct *codearts.Account, model string, err error) bool {
	var p *provider.Error
	if !errors.As(err, &p) {
		return false
	}
	if p.AccountFault {
		s.Store.MarkAccountResult(acct.ID, false, p.Error())
		s.Store.CooldownAccount(acct.ID, p.Error(), s.Store.Settings().Pool.BreakerCooldownSec)
	} else if p.Status == 429 || p.Status >= 500 || p.Status == 404 {
		s.Store.MarkModelResult(acct.ID, model, false, p.Error(), s.Store.Settings().Pool.RateCooldownSec)
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
