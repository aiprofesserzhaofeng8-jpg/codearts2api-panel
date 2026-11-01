package server

import (
	"encoding/json"
	"net/http"

	"codearts2api/internal/accounts"
)

func (s *Server) handleAdminLoginComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		PreviewID string `json:"preview_id"`
		Replace   bool   `json:"replace_existing"`
		Name      string `json:"name"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body) != nil || body.SessionID == "" || body.PreviewID == "" {
		writeErr(w, 400, "请先检查授权并核对账号", "invalid_request_error")
		return
	}
	id, err := s.BrowserLogins.Confirm(accounts.BrowserLoginConfirmation{SessionID: body.SessionID, PreviewID: body.PreviewID, Name: body.Name, Replace: body.Replace})
	if err != nil {
		writeLoginError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "saved": true, "account_id": id, "message": "账号已保存"})
}
