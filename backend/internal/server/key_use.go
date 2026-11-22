package server

import (
	"errors"
	"net/http"

	"codearts2api/internal/access"
)

func (s *Server) keyUseModels(admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, 405, "仅支持 GET", "invalid_request_error")
			return
		}
		ref := r.URL.Query().Get("ref")
		var key string
		if admin {
			for _, k := range s.Store.Keys() {
				if keyReference(k.Key) == ref {
					key = k.Key
					break
				}
			}
		} else {
			key, _ = s.Store.RevealUserKey(currentUser(r).ID, ref)
		}
		if key == "" {
			writeErr(w, 404, "密钥不存在", "invalid_request_error")
			return
		}
		scope, err := s.Store.AccessScope(key)
		if errors.Is(err, access.ErrGroupUnavailable) {
			writeErr(w, 403, err.Error(), "permission_error", "group_unavailable")
			return
		}
		if err != nil {
			writeErr(w, 403, "密钥或分组不可用", "permission_error")
			return
		}
		ids := []string{}
		settings := s.Store.Settings()
		for _, m := range s.Routing.ModelsForScope(scope) {
			if settings.ForceModel != "" && m.ID != settings.ForceModel {
				continue
			}
			if _, _, err := s.Routing.Resolve(key, settings, m.ID); err == nil {
				ids = append(ids, m.ID)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"models": ids})
	}
}
