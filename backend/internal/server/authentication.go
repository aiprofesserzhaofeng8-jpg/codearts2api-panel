package server

import (
	"codearts2api/internal/access"
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const sessionCookie = "codearts_session"

func sessionSameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		return err == nil && parsed.User == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == r.Host
	}
	return true
}
func (s *Server) authenticatedPrincipal(r *http.Request) (access.UserView, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || s.Users == nil {
		return access.UserView{}, false
	}
	return s.Users.Authenticate(c.Value)
}

// OAuth binding and consumer operations never accept administrator identities.
func (s *Server) authenticatedUser(r *http.Request) (access.UserView, bool) {
	u, ok := s.authenticatedPrincipal(r)
	return u, ok && u.Role == access.RoleUser
}
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	if !sessionSameOrigin(r) {
		writeErr(w, 403, "跨站登录请求被拒绝", "permission_error")
		return
	}
	if s.loginFailures.blocked(r.RemoteAddr) {
		w.Header().Set("Retry-After", "60")
		writeErr(w, 429, "登录尝试过多，请稍后再试", "rate_limit_error")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeUserBody(w, r, &body) {
		return
	}
	token := rand.Text() + rand.Text()
	u, expires, err := s.Users.Login(r.Context(), body.Username, body.Password, token)
	if err != nil {
		if errors.Is(err, access.ErrUserLoginBusy) {
			w.Header().Set("Retry-After", "2")
			writeErr(w, 429, err.Error(), "rate_limit_error")
			return
		}
		s.loginFailures.failure(r.RemoteAddr)
		s.audit(r, "anonymous", "login", 401)
		writeErr(w, 401, access.ErrUserAuthentication.Error(), "authentication_error")
		return
	}
	s.loginFailures.success(r.RemoteAddr)
	// Replacing a browser session also revokes its previous token.
	if previous, err := r.Cookie(sessionCookie); err == nil {
		s.Users.Logout(previous.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.sessionCookieSecure(r), SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	s.audit(r, string(u.Role)+":"+u.ID, "login", 200)
	writeJSON(w, 200, map[string]any{"ok": true, "user": u, "expires_at": expires.Unix()})
}
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	if !sessionSameOrigin(r) {
		writeErr(w, 403, "跨站请求被拒绝", "permission_error")
		return
	}
	u, ok := s.authenticatedPrincipal(r)
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.Users.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", Value: "", HttpOnly: true, Secure: s.sessionCookieSecure(r), SameSite: http.SameSiteStrictMode, MaxAge: -1})
	if ok {
		s.audit(r, string(u.Role)+":"+u.ID, "logout", 200)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	if !sessionSameOrigin(r) {
		writeErr(w, 403, "跨站会话请求被拒绝", "permission_error")
		return
	}
	u, ok := s.authenticatedPrincipal(r)
	if !ok {
		writeJSON(w, 200, map[string]bool{"authenticated": false})
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": true, "user": u})
}

func (s *Server) sessionCookieSecure(r *http.Request) bool {
	return r.TLS != nil || strings.HasPrefix(s.Store.IdentitySettings().PublicOrigin, "https://")
}
