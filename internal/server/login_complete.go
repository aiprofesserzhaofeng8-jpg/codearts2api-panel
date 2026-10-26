package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"codearts2api/internal/codearts"
	"codearts2api/internal/store"
)

func loginFingerprint(sess *codearts.Session) string {
	sum := sha256.Sum256([]byte(sess.DomainID + "\x00" + sess.AccessKey))
	return hex.EncodeToString(sum[:])
}

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
	f := s.Logins.Get(body.SessionID)
	if f == nil {
		writeErr(w, 404, "登录会话已失效，请重新获取链接", "invalid_request_error")
		return
	}
	id, err := f.Commit(func(sess *codearts.Session) (string, error) {
		if body.PreviewID != loginFingerprint(sess) {
			return "", fmt.Errorf("账号预览已变化，请重新检查")
		}
		name := body.Name
		if name == "" {
			name = sess.AccountLabel
		}
		a := &codearts.Account{ID: codearts.RandomHex(8), Name: name, Source: "browser", Enabled: true, Status: "ok", Session: sess}
		_, err := s.Store.UpsertAccountAndSave(a, body.Replace)
		if err != nil {
			if errors.Is(err, store.ErrAccountExists) {
				return "", err
			}
			return "", fmt.Errorf("凭据保存失败，请检查目录权限后重试")
		}
		s.Cred.Invalidate(a.ID)
		return a.ID, nil
	})
	if err != nil {
		writeErr(w, 409, err.Error(), "invalid_request_error")
		return
	}
	// A replay after manual deletion must not resurrect a previously saved account.
	if s.Store.Account(id) == nil {
		writeErr(w, 410, "该次登录保存的账号已被删除，请重新发起登录", "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "saved": true, "account_id": id, "message": "账号已保存"})
}
