package server

import (
	"codearts2api/internal/config"
	"net/http"
)

func (s *Server) handleDefaultModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		value, err := s.UnifiedModels.DefaultModels()
		if err != nil {
			writeModelError(w, err, 400)
			return
		}
		writeJSON(w, 200, value)
	case http.MethodPost:
		var body config.DefaultModelsApply
		if _, ok := decodeModelObject(w, r, 128<<10, &body, "version", "catalog_revision", "expected_routing_revision", "names", "group_id", "expected_group_revision"); !ok {
			return
		}
		snap, err := s.UnifiedModels.ApplyDefaultModels(body)
		if err != nil {
			writeModelError(w, err, 400)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "routing_revision": snap.Revision})
	default:
		writeErr(w, 405, "不支持的方法", "invalid_request_error")
	}
}
