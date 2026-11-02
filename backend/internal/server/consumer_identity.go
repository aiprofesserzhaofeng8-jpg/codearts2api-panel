package server

import (
	"codearts2api/internal/access"
	"crypto/rand"
	"net/http"
	"net/url"
)

func (s *Server) handleAdminIdentitySettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		cfg := s.Store.IdentitySettings()
		smtp, github, linuxdo := cfg.SMTPPassword != "", cfg.GitHub.ClientSecret != "", cfg.LinuxDO.ClientSecret != ""
		cfg.SMTPPassword = ""
		cfg.GitHub.ClientSecret = ""
		cfg.LinuxDO.ClientSecret = ""
		writeJSON(w, 200, map[string]any{"settings": cfg, "smtp_password_set": smtp, "github_secret_set": github, "linuxdo_secret_set": linuxdo})
		return
	}
	if !consumerMethod(w, r, "POST") {
		return
	}
	var b struct {
		Settings     access.IdentitySettings `json:"settings"`
		ClearSMTP    bool                    `json:"clear_smtp"`
		ClearGitHub  bool                    `json:"clear_github"`
		ClearLinuxDO bool                    `json:"clear_linuxdo"`
	}
	if !decodeUserBody(w, r, &b) {
		return
	}
	consumerResult(w, s.Store.SaveIdentitySettings(b.Settings, b.ClearSMTP, b.ClearGitHub, b.ClearLinuxDO))
}
func (s *Server) handleIdentityOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !consumerMethod(w, r, "GET") {
		return
	}
	cfg := s.Store.IdentitySettings()
	writeJSON(w, 200, map[string]bool{"email": cfg.SMTPHost != "", "github": cfg.GitHub.Enabled, "linuxdo": cfg.LinuxDO.Enabled})
}
func (s *Server) handleUserIdentity(w http.ResponseWriter, r *http.Request) {
	if s.Identity == nil {
		writeErr(w, 503, "身份服务未就绪", "server_error")
		return
	}
	u := currentUser(r)
	if r.Method == "GET" {
		writeJSON(w, 200, map[string]any{"identities": s.Store.UserIdentities(u.ID)})
		return
	}
	if !consumerMethod(w, r, "POST") {
		return
	}
	var b struct {
		Action   string `json:"action"`
		Email    string `json:"email"`
		Token    string `json:"token"`
		Code     string `json:"code"`
		Provider string `json:"provider"`
	}
	if !decodeUserBody(w, r, &b) {
		return
	}
	switch b.Action {
	case "send":
		token, err := s.Identity.StartEmail(r.Context(), u.ID, b.Email, "verify")
		if err != nil {
			consumerResult(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "token": token})
	case "verify":
		consumerResult(w, s.Identity.VerifyEmail(u.ID, b.Token, b.Code))
	case "unbind":
		consumerResult(w, s.Store.UnbindIdentity(u.ID, u.Revision, b.Provider))
	default:
		writeErr(w, 400, "不支持的身份操作", "invalid_request_error")
	}
}
func (s *Server) handlePasswordRecovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !consumerMethod(w, r, "POST") {
		return
	}
	if !sessionSameOrigin(r) {
		writeErr(w, 403, "跨站请求被拒绝", "permission_error")
		return
	}
	if s.Identity == nil {
		writeErr(w, 503, "身份服务未就绪", "server_error")
		return
	}
	if s.consumerFailures.blocked(r.RemoteAddr) {
		writeErr(w, 429, "尝试过多，请稍后再试", "rate_limit_error")
		return
	}
	var b struct {
		Action   string `json:"action"`
		Email    string `json:"email"`
		Token    string `json:"token"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if !decodeUserBody(w, r, &b) {
		return
	}
	s.consumerFailures.failure(r.RemoteAddr)
	if b.Action == "send" {
		token, err := s.Identity.StartEmail(r.Context(), "", b.Email, "reset")
		if err != nil {
			consumerResult(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "token": token, "message": "如果邮箱已绑定，将收到验证码"})
		return
	}
	if b.Action != "reset" {
		writeErr(w, 400, "不支持的找回密码操作", "invalid_request_error")
		return
	}
	consumerResult(w, s.Identity.ResetPassword(r.Context(), b.Token, b.Code, b.Password))
}

const oauthCookie = "consumer_oauth_browser"

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !consumerMethod(w, r, "POST") {
		return
	}
	if !sessionSameOrigin(r) {
		writeErr(w, 403, "跨站请求被拒绝", "permission_error")
		return
	}
	if s.Identity == nil {
		writeErr(w, 503, "身份服务未就绪", "server_error")
		return
	}
	if s.consumerFailures.blocked(r.RemoteAddr) {
		writeErr(w, 429, "登录尝试过多，请稍后再试", "rate_limit_error")
		return
	}
	s.consumerFailures.failure(r.RemoteAddr)
	var b struct {
		Provider string `json:"provider"`
		Bind     bool   `json:"bind"`
	}
	if !decodeUserBody(w, r, &b) {
		return
	}
	id := ""
	if b.Bind {
		u, ok := s.authenticatedUser(r)
		if !ok {
			writeErr(w, 401, "请先登录", "authentication_error")
			return
		}
		id = u.ID
	}
	browser := rand.Text() + rand.Text()
	target, err := s.Identity.StartOAuth(b.Provider, id, browser)
	if err != nil {
		consumerResult(w, err)
		return
	}
	// Lax cookie is intentionally limited to OAuth correlation; user sessions stay Strict.
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: browser, Path: "/auth/oauth", HttpOnly: true, Secure: s.sessionCookieSecure(r), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	writeJSON(w, 200, map[string]any{"url": target})
}
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !consumerMethod(w, r, "GET") {
		return
	}
	fail := func(message string) {
		http.Redirect(w, r, "/user?oauth_error="+url.QueryEscape(message), http.StatusSeeOther)
	}
	if s.Identity == nil {
		fail("身份服务未就绪")
		return
	}
	cookie, err := r.Cookie(oauthCookie)
	if err != nil {
		fail("登录验证已过期，请重试")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/auth/oauth", HttpOnly: true, Secure: s.sessionCookieSecure(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	u, bound, err := s.Identity.CompleteOAuth(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"), cookie.Value)
	if err != nil {
		fail(err.Error())
		return
	}
	if !bound {
		token := rand.Text() + rand.Text()
		if _, _, err = s.Users.OpenSession(u.ID, u.Revision, token); err != nil {
			fail("用户权限已变更，请重新登录")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.sessionCookieSecure(r), SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	}
	s.audit(r, "user:"+u.ID, "oauth_login_or_bind", 200)
	http.Redirect(w, r, "/user?oauth_success=1", http.StatusSeeOther)
}
