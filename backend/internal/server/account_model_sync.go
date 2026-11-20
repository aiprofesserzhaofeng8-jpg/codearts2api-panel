package server

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/catalog"
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

func (s *Server) accountCatalogModels(ctx context.Context, a *accounts.Account) ([]provider.Model, error) {
	if a.ProviderID() == provider.CodeArts {
		return s.catalogRefreshOperations().Native(ctx, a)
	}
	adapter, raw, err := s.providerCredentials(ctx, a)
	if err != nil {
		return nil, err
	}
	if special, ok := adapter.(interface {
		AccountCatalog(context.Context, json.RawMessage) ([]provider.Model, error)
	}); ok {
		return special.AccountCatalog(ctx, raw)
	}
	return adapter.Models(ctx, raw)
}
func (s *Server) handleAccountModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "POST" {
		writeErr(w, 405, "仅支持 GET 和 POST", "invalid_request_error")
		return
	}
	id := r.URL.Query().Get("id")
	a := s.Store.Account(id)
	if a == nil {
		writeErr(w, 404, "账号不存在", "invalid_request_error")
		return
	}
	if a.ProviderID() != provider.CodeArts {
		if s.Providers == nil {
			writeErr(w, 503, "上游适配器尚未就绪", "server_error")
			return
		}
		if _, ok := s.Providers.Get(a.ProviderID()); !ok {
			writeErr(w, 400, "该平台尚未接入", "invalid_request_error")
			return
		}
	}
	if r.Method == "GET" {
		_, v := s.Store.ModelSyncTarget(id)
		writeJSON(w, 200, map[string]any{"snapshot": v})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	release, err := s.modelSyncLocks.Acquire(ctx, id)
	if err != nil {
		writeErr(w, 408, "等待账号同步超时", "timeout")
		return
	}
	defer release()
	target, old := s.Store.ModelSyncTarget(id)
	got, err := s.Catalog.DiscoverAccountModels(ctx, id, s.accountCatalogModels)
	if err != nil {
		if errors.Is(err, accounts.ErrNoAccount) {
			writeErr(w, 409, "账号已删除或停用", "invalid_request_error")
			return
		}
		msg := "上游目录读取失败"
		// A platform may answer with an actionable reason (for example an account
		// that is authorized but not provisioned yet). Showing it is the point of
		// the button; a generic sentence leaves the operator with no next step.
		var upstream *provider.Error
		if errors.As(err, &upstream) && upstream.Message != "" {
			msg = upstream.Message
		} else if errors.Is(err, context.DeadlineExceeded) {
			msg = "上游目录读取超时"
		}
		writeErr(w, 502, msg, "upstream_error")
		return
	}
	var prev *catalog.AccountModelList
	if old != nil {
		prev = (*catalog.AccountModelList)(&old.List)
	}
	delta, err := got.Diff(prev)
	if err != nil {
		writeErr(w, 409, err.Error(), "conflict")
		return
	}
	snapshot := &accounts.ModelSnapshot{List: accounts.ModelList(*got), First: old == nil, Delta: delta}
	if old != nil {
		snapshot.Published = old.Published
	}
	if snapshot.Published == nil {
		snapshot.Published = make(map[string]accounts.ModelObservation)
	}
	for _, m := range got.Models {
		if got.Kind != "live" {
			break
		}
		full := provider.ModelID(got.Provider, got.Scope, m.ID)
		_, base := s.Reg.ResolveRaw(full)
		if !base || s.Store.IsPublishedModel(full) {
			snapshot.Published[m.ID] = m
		}
	}
	if err = s.Store.SaveModelSnapshot(target, snapshot); err != nil {
		if errors.Is(err, accounts.ErrCredentialChanged) {
			writeErr(w, 409, err.Error(), "conflict")
		} else {
			writeErr(w, 500, "快照保存失败", "server_error")
		}
		return
	}
	writeJSON(w, 200, map[string]any{"snapshot": snapshot})
}
