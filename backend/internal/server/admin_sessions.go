package server

import (
	"bytes"
	"codearts2api/internal/codearts"
	"codearts2api/internal/store"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"
)

const adminCookie = "codearts_admin_session"

func (s *Server) validAdminSession(r *http.Request) bool {
	cookie, err := r.Cookie(adminCookie)
	if err != nil {
		return false
	}
	return s.AdminSessions.Valid(cookie.Value, func() string { return s.Store.Settings().AdminPassword })
}
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	if !s.adminSameOrigin(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeErr(w, 403, "跨站登录请求被拒绝", "invalid_request_error")
		return
	}
	if s.adminFailures.blocked(r.RemoteAddr) {
		w.Header().Set("Retry-After", "60")
		writeErr(w, 429, "登录尝试过多，请稍后再试", "rate_limit_error")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
		writeErr(w, 400, "登录参数无效", "invalid_request_error")
		return
	}
	password := s.Store.Settings().AdminPassword
	if !store.VerifyAdminPassword(password, body.Password) {
		s.adminFailures.failure(r.RemoteAddr)
		s.audit(r, "anonymous", "login", 401)
		writeErr(w, 401, "管理密码不正确", "authentication_error")
		return
	}
	s.adminFailures.success(r.RemoteAddr)
	token := codearts.RandomHex(32)
	now := time.Now()
	expires := s.AdminSessions.Issue(token, password, now)
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	s.audit(r, "admin_session", "login", 200)
	writeJSON(w, 200, map[string]any{"ok": true, "expires_at": expires.Unix()})
}
func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	if !s.adminSameOrigin(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeErr(w, 403, "跨站请求被拒绝", "invalid_request_error")
		return
	}
	authenticated := s.validAdminSession(r)
	if c, e := r.Cookie(adminCookie); e == nil {
		s.AdminSessions.Revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: "", Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	if authenticated {
		s.audit(r, "admin_session", "logout", 200)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (s *Server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": s.validAdminSession(r), "local_compatibility": s.AllowLocalAdmin})
}
func (s *Server) audit(r *http.Request, actor, operation string, status int) {
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.Store.Audit(store.AuditEvent{Actor: actor, Operation: operation, Path: r.URL.Path, Peer: peer, Status: status})
}

type auditWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(b)
}
func (s *Server) serveAudited(w http.ResponseWriter, r *http.Request, actor string, next http.HandlerFunc) {
	if r.Method == "GET" || r.Method == "HEAD" {
		next(w, r)
		return
	}
	operation := r.Method
	if r.Body != nil {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			writeErr(w, 413, "管理请求过大", "invalid_request_error")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var action struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(raw, &action)
		switch action.Action {
		case "start", "check", "complete", "cancel", "export", "remove", "pause", "resume", "manual", "session-json", "import", "toggle", "clear-errors", "reimport", "probe":
			operation = action.Action
		}
	}
	recorder := &auditWriter{ResponseWriter: w}
	next(recorder, r)
	status := recorder.status
	if status == 0 {
		status = 200
	}
	s.audit(r, actor, operation, status)
}
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeErr(w, 405, "审计记录只读", "invalid_request_error")
		return
	}
	events := s.Store.AuditEvents()
	size := intQuery(r, "page_size")
	if size <= 0 {
		size = 50
	}
	size = min(size, 200)
	page := intQuery(r, "page")
	if page <= 0 {
		page = 1
	}
	page = min(page, max(1, (len(events)+size-1)/size))
	start := min((page-1)*size, len(events))
	end := min(start+size, len(events))
	writeJSON(w, 200, map[string]any{"events": events[start:end], "total": len(events), "page": page, "page_size": size})
}
