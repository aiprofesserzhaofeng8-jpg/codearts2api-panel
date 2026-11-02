package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/codearts"
	"codearts2api/internal/storage"
)

const userCookie = "codearts_user_session"

type userContextKey struct{}
type inferenceUserKey struct{}

func inferenceUser(ctx context.Context) string {
	id, _ := ctx.Value(inferenceUserKey{}).(string)
	return id
}
func currentUser(r *http.Request) access.UserView {
	return r.Context().Value(userContextKey{}).(access.UserView)
}
func decodeUserBody(w http.ResponseWriter, r *http.Request, out any) bool {
	return decodeUserBodyLimit(w, r, out, 16384)
}
func decodeUserBodyLimit(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		writeErr(w, 400, "请求参数无效", "invalid_request_error")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		writeErr(w, 400, "请求必须为单个 JSON 对象", "invalid_request_error")
		return false
	}
	return true
}
func userSameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		return err == nil && parsed.User == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == r.Host
	}
	return true
}
func (s *Server) authenticatedUser(r *http.Request) (access.UserView, bool) {
	c, err := r.Cookie(userCookie)
	if err != nil || s.Users == nil {
		return access.UserView{}, false
	}
	return s.Users.Authenticate(c.Value)
}
func (s *Server) withUserAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		u, ok := s.authenticatedUser(r)
		if !ok {
			writeErr(w, 401, "请登录用户账号", "authentication_error")
			return
		}
		if !userSameOrigin(r) {
			writeErr(w, 403, "跨站用户请求被拒绝", "permission_error")
			return
		}
		s.serveAudited(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, u)), "user:"+u.ID, next)
	}
}
func (s *Server) handleUserLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	if !userSameOrigin(r) {
		writeErr(w, 403, "跨站登录请求被拒绝", "permission_error")
		return
	}
	if s.userFailures.blocked(r.RemoteAddr) {
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
		s.userFailures.failure(r.RemoteAddr)
		s.audit(r, "anonymous", "user_login", 401)
		writeErr(w, 401, access.ErrUserAuthentication.Error(), "authentication_error")
		return
	}
	s.userFailures.success(r.RemoteAddr)
	http.SetCookie(w, &http.Cookie{Name: userCookie, Value: token, Path: "/user", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	s.audit(r, "user:"+u.ID, "login", 200)
	writeJSON(w, 200, map[string]any{"ok": true, "user": u, "expires_at": expires.Unix()})
}
func (s *Server) handleUserLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	if !userSameOrigin(r) {
		writeErr(w, 403, "跨站请求被拒绝", "permission_error")
		return
	}
	u, ok := s.authenticatedUser(r)
	if c, err := r.Cookie(userCookie); err == nil {
		s.Users.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: userCookie, Path: "/user", Value: "", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	if ok {
		s.audit(r, "user:"+u.ID, "logout", 200)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) handleUserSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	if !userSameOrigin(r) {
		writeErr(w, 403, "跨站用户请求被拒绝", "permission_error")
		return
	}
	u, ok := s.authenticatedUser(r)
	if !ok {
		writeJSON(w, 200, map[string]bool{"authenticated": false})
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": true, "user": u})
}
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, 200, map[string]any{"users": s.Store.Users()})
	case "POST":
		var update access.UserUpdate
		if !decodeUserBody(w, r, &update) {
			return
		}
		u, err := s.Users.Save(r.Context(), update)
		if err != nil {
			writeErr(w, 400, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "user": u})
	default:
		writeErr(w, 405, "仅支持 GET、POST", "invalid_request_error")
	}
}
func (s *Server) handleUserProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		writeErr(w, 405, "仅支持 GET、POST", "invalid_request_error")
		return
	}
	u := currentUser(r)
	if r.Method == "POST" {
		var update access.ProfileUpdate
		if !decodeUserBodyLimit(w, r, &update, 32*1024) {
			return
		}
		_, err := s.Store.UpdateUserProfile(u.ID, u.Revision, update)
		if err != nil {
			if errors.Is(err, access.ErrUserAuthentication) {
				writeErr(w, 401, "用户权限已变更，请重新登录", "authentication_error")
			} else if errors.Is(err, access.ErrInvalidProfile) {
				writeErr(w, 400, err.Error(), "invalid_request_error")
			} else {
				writeErr(w, 500, "个人资料保存失败，请稍后重试", "server_error")
			}
			return
		}
	}
	view, name, err := s.Store.UserProfile(u.ID)
	if err != nil {
		writeErr(w, 403, "用户分组权限无效，请联系管理员", "permission_error")
		return
	}
	writeJSON(w, 200, map[string]any{"user": view, "group_name": name, "key_limit": 32})
}
func (s *Server) handleUserPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		OldPassword string `json:"old_password"`
		Password    string `json:"password"`
	}
	if !decodeUserBody(w, r, &body) {
		return
	}
	if err := s.Users.ChangePassword(r.Context(), currentUser(r).ID, body.OldPassword, body.Password); err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true, "login_required": true})
}
func safeUserKey(k *storage.APIKey) map[string]any {
	return map[string]any{"ref": keyReference(k.Key), "key": codearts.Mask(k.Key), "name": k.Name, "enabled": k.Enabled, "created_at": k.CreatedAt, "last_used": k.LastUsed, "requests": k.Requests, "budget": k.Budget, "budget_usage": k.BudgetUsage}
}
func (s *Server) handleUserKeys(w http.ResponseWriter, r *http.Request) {
	id := currentUser(r).ID
	switch r.Method {
	case "GET":
		out := []map[string]any{}
		for _, k := range s.Store.UserKeys(id) {
			out = append(out, safeUserKey(k))
		}
		writeJSON(w, 200, map[string]any{"keys": out})
	case "POST":
		var body struct {
			Name string `json:"name"`
		}
		if !decodeUserBody(w, r, &body) {
			return
		}
		k, err := s.Store.AddUserKeyAndSave(id, body.Name)
		if err != nil {
			writeErr(w, 400, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "key": k.Key})
	case "DELETE":
		action := r.URL.Query().Get("action")
		if action != "delete" && action != "toggle" {
			writeErr(w, 400, "无效密钥操作", "invalid_request_error")
			return
		}
		ok, err := s.Store.ChangeUserKeyAndSave(id, r.URL.Query().Get("ref"), action == "delete")
		if err != nil {
			writeErr(w, 500, "密钥保存失败，请稍后再试", "server_error")
			return
		}
		if !ok {
			writeErr(w, 404, "密钥不存在", "invalid_request_error")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeErr(w, 405, "不支持的方法", "invalid_request_error")
	}
}
func (s *Server) handleUserKeyReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Ref string `json:"ref"`
	}
	if !decodeUserBody(w, r, &body) {
		return
	}
	value, ok := s.Store.RevealUserKey(currentUser(r).ID, body.Ref)
	if !ok {
		writeErr(w, 404, "密钥不存在", "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]string{"value": value})
}
func (s *Server) handleUserModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	scope, _, err := s.Store.UserScope(currentUser(r).ID)
	if err != nil {
		writeErr(w, 403, "用户分组权限无效", "permission_error")
		return
	}
	models := []map[string]string{}
	for _, m := range s.Routing.ModelsForScope(scope) {
		models = append(models, map[string]string{"id": m.ID, "name": m.Name})
	}
	writeJSON(w, 200, map[string]any{"models": models, "default_model": scope.ClientDefaultModel})
}
func (s *Server) handleUserUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	q := storage.UsageQuery{Page: 1, PageSize: 20}
	for name, target := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if raw := r.URL.Query().Get(name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || name == "page_size" && n > 100 {
				writeErr(w, 400, "分页参数无效", "invalid_request_error")
				return
			}
			*target = n
		}
	}
	for name, target := range map[string]*int64{"start": &q.Start, "end": &q.End} {
		if raw := r.URL.Query().Get(name); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 {
				writeErr(w, 400, "时间参数无效", "invalid_request_error")
				return
			}
			*target = n
		}
	}
	if q.Start > 0 && q.End > 0 && q.Start >= q.End {
		writeErr(w, 400, "开始时间须早于结束时间", "invalid_request_error")
		return
	}
	q.Location = time.UTC
	if zone := r.URL.Query().Get("timezone"); zone != "" {
		location, err := time.LoadLocation(zone)
		if err != nil {
			writeErr(w, 400, "时区参数无效", "invalid_request_error")
			return
		}
		q.Location = location
	}
	writeJSON(w, 200, s.Store.QueryUserUsage(currentUser(r).ID, q))
}
