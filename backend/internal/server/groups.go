package server

import (
	"context"
	"encoding/json"
	"net/http"

	"codearts2api/internal/access"
	"codearts2api/internal/catalog"
	"codearts2api/internal/registry"
	"codearts2api/internal/storage"
)

type groupKeyContext struct{}

func groupKey(ctx context.Context) string {
	key, _ := ctx.Value(groupKeyContext{}).(string)
	return key
}
func groupDenied() error { return catalog.GroupDenied() }
func (s *Server) visibleModels(ctx context.Context) ([]registry.Model, error) {
	return s.Routing.VisibleModels(groupKey(ctx))
}
func (s *Server) resolveModelForRequest(r *http.Request, settings storage.Settings, requested string) (registry.Model, error) {
	if c, ok := r.Context().Value(usageContextKey{}).(*usageCapture); ok {
		c.requestedModel = requested
	}
	model, plan, err := s.Routing.Resolve(groupKey(r.Context()), settings, requested)
	if plan != nil {
		if c, ok := r.Context().Value(usageContextKey{}).(*usageCapture); ok {
			c.mapping = plan
		}
	}
	return model, err
}
func (s *Server) checkGroupModel(ctx context.Context, model string) error {
	return s.Routing.Check(groupKey(ctx), model, requestKind(ctx) == "search", mappedPlan(ctx), func() bool {
		return s.mappedSummaryFallback(ctx, model)
	})
}
func (s *Server) handleAdminGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"groups": s.AccessManagement.Groups()})
	case http.MethodPost:
		var g access.AccountGroup
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&g) != nil {
			writeErr(w, 400, "分组格式无效", "invalid_request_error")
			return
		}
		saved, err := s.AccessManagement.SaveGroup(g)
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
		if err := s.AccessManagement.DeleteGroup(id); err != nil {
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
	if err := s.AccessManagement.SetKeyGroup(body.Ref, *body.GroupID); err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
