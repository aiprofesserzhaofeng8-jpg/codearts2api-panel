package server

import (
	"codearts2api/internal/codearts"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// Retrievable keys remain available for personal use, but only on explicit action.
func (s *Server) handleAdminKeyReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || body.Key == "" {
		writeErr(w, 400, "缺少密钥标识", "invalid_request_error")
		return
	}
	for _, key := range s.Store.Keys() {
		if keyReference(key.Key) == body.Key || key.Key == body.Key || codearts.Mask(key.Key) == body.Key {
			writeJSON(w, 200, map[string]any{"value": key.Key})
			return
		}
	}
	writeErr(w, 404, "密钥不存在", "invalid_request_error")
}

func keyReference(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "key_" + hex.EncodeToString(sum[:12])
}
