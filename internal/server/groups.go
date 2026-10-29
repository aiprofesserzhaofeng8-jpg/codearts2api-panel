package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

type groupKeyContext struct{}

func groupKey(ctx context.Context) string {
	key, _ := ctx.Value(groupKeyContext{}).(string)
	return key
}
func groupDenied() error {
	return &provider.Error{Status: 403, Message: "当前 API Key 的分组不允许调用此模型或没有对应账号"}
}
func scopeAllows(scope store.AccessScope, accounts []*codearts.Account, model registry.Model) bool {
	if !scope.Restricted {
		return true
	}
	if !scope.AllowsModel(model.ID) {
		return false
	}
	for _, a := range accounts {
		if scope.AccountIDs[a.ID] && a.Enabled && a.ProviderID() == modelProvider(model) && (a.ProviderID() == provider.CodeArts || a.Scope == model.Scope) {
			return true
		}
	}
	return false
}
func (s *Server) visibleModels(ctx context.Context) ([]registry.Model, error) {
	access, err := s.Store.AccessScope(groupKey(ctx))
	if err != nil {
		return nil, groupDenied()
	}
	settings := s.Store.Settings()
	accounts := s.Store.Accounts()
	out := []registry.Model{}
	for _, m := range s.Reg.List(settings.ModelAliases) {
		resolved, ok := s.Reg.Resolve(m.ID, settings.ModelAliases)
		if ok && scopeAllows(access, accounts, resolved) {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *Server) resolveModelForRequest(r *http.Request, settings store.Settings, requested string) (registry.Model, error) {
	access, err := s.Store.AccessScope(groupKey(r.Context()))
	if err != nil {
		return registry.Model{}, groupDenied()
	}
	m, err := s.resolveModel(settings, requested)
	if err != nil {
		var missing *apierr.ModelNotFound
		if access.Restricted && errors.As(err, &missing) {
			list, _ := s.visibleModels(r.Context())
			ids := []string{}
			for _, m := range list {
				ids = append(ids, m.ID)
			}
			return registry.Model{}, &apierr.ModelNotFound{Name: requested, Available: ids}
		}
		return registry.Model{}, err
	}
	if !scopeAllows(access, s.Store.Accounts(), m) {
		return registry.Model{}, groupDenied()
	}
	return m, nil
}
func (s *Server) checkGroupModel(ctx context.Context, model string) error {
	access, err := s.Store.AccessScope(groupKey(ctx))
	if err != nil {
		return groupDenied()
	}
	if !access.Restricted {
		return nil
	}
	if requestKind(ctx) == "search" {
		target, _, err := resolveSearchTarget(s.Store.Settings(), model)
		if err != nil || target.ID != model || !scopeAllows(access, s.Store.Accounts(), target) {
			return groupDenied()
		}
		return nil
	}
	m, ok := s.Reg.Resolve(model, s.Store.Settings().ModelAliases)
	if !ok || !scopeAllows(access, s.Store.Accounts(), m) {
		return groupDenied()
	}
	return nil
}
func (s *Server) handleAdminGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"groups": s.Store.Groups()})
	case http.MethodPost:
		var g store.AccountGroup
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&g) != nil {
			writeErr(w, 400, "分组格式无效", "invalid_request_error")
			return
		}
		for i, id := range g.ModelIDs {
			if m, ok := s.Reg.Resolve(strings.TrimSpace(id), s.Store.Settings().ModelAliases); ok {
				g.ModelIDs[i] = m.ID
			}
		}
		saved, err := s.Store.SaveGroup(g)
		if err != nil {
			writeErr(w, 400, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "group": saved})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeErr(w, 400, "缺少分组标识", "invalid_request_error")
			return
		}
		if err := s.Store.DeleteGroup(id); err != nil {
			writeErr(w, 409, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeErr(w, 405, "仅支持 GET/POST/DELETE", "invalid_request_error")
	}
}
func (s *Server) handleKeyGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Ref     string  `json:"ref"`
		GroupID *string `json:"group_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || body.Ref == "" || body.GroupID == nil {
		writeErr(w, 400, "缺少密钥或分组设置", "invalid_request_error")
		return
	}
	if err := s.Store.SetKeyGroupAndSave(s.resolveKeyRef(body.Ref), *body.GroupID); err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
