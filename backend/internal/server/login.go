package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codearts2api/internal/codearts"
)

// --- 每日签到 ---

// handleAdminClaim 手动触发一次签到（全部账号或指定账号）。
func (s *Server) handleAdminClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		writeErr(w, 400, "领取请求格式无效", "invalid_request_error")
		return
	}

	targets := s.Store.Accounts()
	if body.ID != "" {
		targets = nil
		if a := s.Store.Account(body.ID); a != nil {
			targets = append(targets, a)
		} else {
			writeErr(w, 404, "账号不存在", "invalid_request_error")
			return
		}
	}
	today := time.Now().Format("2006-01-02")
	results := make([]map[string]any, 0, len(targets))
	okCount := 0
	for _, a := range targets {
		if body.ID == "" && (!a.Enabled || !s.dailyRewardsSupported(a)) {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		msg, err := s.ClaimForAccount(ctx, a, today)
		cancel()
		row := map[string]any{"id": a.ID, "name": a.DisplayName(), "ok": err == nil, "provider": a.ProviderID()}
		if err != nil {
			row["message"] = codearts.DescribeClaimError(err)
		} else {
			row["message"] = msg
			okCount++
		}
		results = append(results, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "date": today, "succeeded": okCount, "total": len(results), "results": results,
	})
}

// --- 本地浏览器登录 ---

// handleAdminLoginStart 启动一次浏览器登录，返回待打开的授权 URL。
func (s *Server) handleAdminLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		PortalHost string `json:"portal_host"`
		Locale     string `json:"locale"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	flow, err := s.BrowserLogins.Start(body.PortalHost, body.Locale)
	if err != nil {
		writeLoginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"session_id":  flow.ID,
		"auth_url":    flow.AuthURL,
		"port":        flow.Port,
		"callback":    flow.Callback,
		"expires_in":  int(5 * time.Minute / time.Second),
		"hint":        flow.Hint,
		"tunnel_hint": "ssh -N -L " + strconv.Itoa(flow.Port) + ":127.0.0.1:" + strconv.Itoa(flow.Port) + " <user>@<server>",
	})
}

// handleAdminLoginPoll 只读取登录状态和账号预览；明确确认后才保存。
func (s *Server) handleAdminLoginPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id", "invalid_request_error")
		return
	}
	preview, err := s.BrowserLogins.Poll(body.SessionID)
	if err != nil {
		writeLoginError(w, err)
		return
	}
	resp := map[string]any{"status": preview.Status, "message": preview.Message}
	if preview.Ready {
		resp["ready"] = true
		resp["saved"] = preview.Saved
		resp["account_id"] = preview.AccountID
		resp["account"] = preview.AccountName
		resp["account_name"] = preview.AccountName
		resp["access_key"] = preview.KeyHint
		resp["domain_id"] = preview.IdentityHint
		resp["expires_at"] = preview.ExpiresAt
		resp["has_refresh"] = preview.HasRefresh
		resp["preview_id"] = preview.PreviewID
		resp["existing"] = preview.Existing
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAdminLoginCallback 接收用户手工粘贴的回调 URL（远端部署无隧道时的兜底）。
func (s *Server) handleAdminLoginCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		URL       string `json:"url"`
		Input     string `json:"input"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体解析失败", "invalid_request_error")
		return
	}
	if body.Input != "" {
		body.URL = body.Input
	}
	if body.SessionID == "" || strings.TrimSpace(body.URL) == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id 或 url", "invalid_request_error")
		return
	}
	result, err := s.BrowserLogins.Submit(r.Context(), body.SessionID, body.URL)
	if err != nil {
		writeLoginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": true, "status": result.Status, "message": result.Message})
}

// handleAdminLoginCancel 取消一次登录。
func (s *Server) handleAdminLoginCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ok := s.BrowserLogins.Cancel(body.SessionID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok})
}
