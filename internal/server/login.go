package server

import (
	"context"
	"encoding/json"
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
	_ = json.NewDecoder(r.Body).Decode(&body)

	targets := s.Store.Accounts()
	if body.ID != "" {
		targets = nil
		if a := s.Store.Account(body.ID); a != nil {
			targets = append(targets, a)
		}
	}
	today := time.Now().Format("2006-01-02")
	results := make([]map[string]any, 0, len(targets))
	okCount := 0
	for _, a := range targets {
		if a.ProviderID() != "codearts" && body.ID == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		msg, err := s.ClaimForAccount(ctx, a, today)
		cancel()
		row := map[string]any{"id": a.ID, "name": a.DisplayName(), "ok": err == nil}
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

	st := s.Store.Settings()
	flow, err := s.Logins.Start(codearts.LoginOptions{
		PortalHost: body.PortalHost,
		Locale:     body.Locale,
		Port:       st.LoginPort,
		// Login uses the authentication extension version, not the IDE version.
		PluginVersion: codearts.LoginPluginVersion,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "启动登录失败: "+err.Error(), "server_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"session_id":  flow.ID,
		"auth_url":    flow.AuthURL,
		"port":        flow.Port,
		"callback":    "http://127.0.0.1:" + strconv.Itoa(flow.Port) + codearts.AuthRedirectPath,
		"expires_in":  int(5 * time.Minute / time.Second),
		"hint":        flow.CallbackHint(),
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
	flow := s.Logins.Get(body.SessionID)
	if flow == nil {
		writeErr(w, http.StatusNotFound, "登录会话不存在或已过期", "invalid_request_error")
		return
	}

	status, msg, account, sess := flow.Snapshot()
	resp := map[string]any{"status": string(status), "message": msg}

	if status == codearts.LoginConfirmed && sess != nil {
		resp["ready"] = true
		resp["saved"] = flow.SavedAccountID() != ""
		resp["account_id"] = flow.SavedAccountID()
		resp["account"] = account
		resp["account_name"] = account
		resp["access_key"] = codearts.Mask(sess.AccessKey)
		resp["domain_id"] = codearts.Mask(sess.DomainID)
		resp["expires_at"] = sess.ExpiresAt.Unix()
		resp["has_refresh"] = sess.HasRefresh()
		resp["preview_id"] = loginFingerprint(sess)
		resp["existing"] = s.Store.FindAccountSession(sess) != nil
		resp["message"] = "授权已验证，请核对账号后点击确认完成登录"
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
	flow := s.Logins.Get(body.SessionID)
	if flow == nil {
		writeErr(w, http.StatusNotFound, "登录会话不存在或已过期，请重新发起登录", "invalid_request_error")
		return
	}
	input := strings.TrimSpace(body.URL)
	var err error
	switch {
	case strings.HasPrefix(input, "{"):
		var sess *codearts.Session
		var expiry struct {
			Expires string `json:"expires_at"`
		}
		_ = json.Unmarshal([]byte(input), &expiry)
		expires, expiryErr := time.Parse(time.RFC3339Nano, expiry.Expires)
		if expiryErr != nil || !expires.After(time.Now()) {
			writeErr(w, 400, "session JSON 缺少有效的 expires_at 时间，请粘贴完整凭据", "invalid_request_error")
			return
		}
		sess, err = codearts.SessionFromJSON([]byte(input))
		if err != nil || sess == nil || !sess.Valid(0) || sess.SecurityToken == "" {
			writeErr(w, 400, "需要有效且完整的 STS session JSON（含 accessKey、secretKey、securitytoken 和 expires_at）", "invalid_request_error")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		_, name, domain, verifyErr := s.Up.CallerIdentity(ctx, sess)
		cancel()
		if verifyErr != nil || domain == "" {
			writeErr(w, 400, "凭据未通过华为身份验证，请检查完整性和有效期", "invalid_request_error")
			return
		}
		if sess.DomainID != "" && sess.DomainID != domain {
			writeErr(w, 400, "凭据与账号身份不一致", "invalid_request_error")
			return
		}
		sess.DomainID = domain
		sess.AccountLabel = name
		sess.Source = "browser-import"
		err = flow.StageSession(sess)
	case strings.Contains(input, "://"):
		_, err = flow.SubmitCallbackURL(input)
	default:
		err = flow.SubmitCode(input)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	status, message, _, _ := flow.Snapshot()
	if status == codearts.LoginConfirmed {
		message = "授权已验证，请检查账号并确认保存"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": true, "status": string(status), "message": message})
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
	ok := s.Logins.Cancel(body.SessionID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok})
}
