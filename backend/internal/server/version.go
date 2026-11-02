package server

import (
	"codearts2api/internal/releaseinfo"
	"encoding/json"
	"net/http"
)

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	checker := s.ReleaseInfo
	if checker == nil {
		checker = releaseinfo.New("unknown")
	}
	result := checker.Status()
	if r.URL.Query().Get("check") == "1" {
		result = checker.Check(r.Context())
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}
