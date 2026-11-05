package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

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
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"groups": s.AccessManagement.CanonicalGroups()})
	case http.MethodPost:
		var body struct {
			access.AccountGroup
			ExpectedRevision uint64 `json:"expected_revision"`
		}
		fields, ok := decodeModelObject(w, r, 256<<10, &body)
		if !ok {
			return
		}
		if _, legacy := fields["model_mappings"]; legacy {
			writeErr(w, 400, "旧模型映射写入已移除，请使用统一模型授权", "invalid_request_error", "legacy_mapping_field")
			return
		}
		for _, field := range []string{"unified_models", "raw_model_mode", "raw_models_visible_default", "expected_revision"} {
			if value, present := fields[field]; present && string(value) == "null" {
				writeErr(w, 400, "分组授权字段不能为 null", "invalid_request_error", "invalid_model_definition")
				return
			}
		}
		g := body.AccountGroup
		if _, present := fields["raw_model_mode"]; present && g.RawModelMode != "legacy" && g.RawModelMode != "qualified_only" {
			writeErr(w, 400, "原始模型模式无效", "invalid_request_error", "invalid_model_definition")
			return
		}
		for _, old := range s.AccessManagement.CanonicalGroups() {
			if old.ID == g.ID {
				if _, ok := fields["raw_model_mode"]; !ok {
					g.RawModelMode = old.RawModelMode
				}
				if _, ok := fields["raw_models_visible_default"]; !ok {
					g.RawModelsVisibleDefault = old.RawModelsVisibleDefault
				}
				if _, ok := fields["default_model"]; !ok {
					g.DefaultModel = old.DefaultModel
				}
				if _, ok := fields["client_default_model"]; !ok {
					g.ClientDefaultModel = old.ClientDefaultModel
				}
			}
		}
		saved, err := s.AccessManagement.SaveUnifiedGroup(g, body.ExpectedRevision)
		if err != nil {
			writeModelError(w, err, 400)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "group": saved})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeErr(w, 400, "缺少分组标识", "invalid_request_error")
			return
		}
		expected, err := strconv.ParseUint(r.URL.Query().Get("expected_revision"), 10, 64)
		if r.URL.Query().Get("expected_revision") == "" {
			writeErr(w, 428, "缺少分组版本条件", "invalid_request_error", "revision_required")
			return
		}
		if err != nil {
			writeErr(w, 400, "分组版本无效", "invalid_request_error")
			return
		}
		if err := s.AccessManagement.DeleteUnifiedGroup(id, expected); err != nil {
			writeModelError(w, err, 409)
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

func (s *Server) handleMappingPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	writeJSON(w, 200, s.Routing.Presets())
}
