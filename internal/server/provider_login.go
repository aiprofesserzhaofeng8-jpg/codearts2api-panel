package server

import (
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type providerLoginFlow struct {
	mu                          sync.Mutex
	provider, scope, state, url string
	created                     time.Time
	credentials                 json.RawMessage
	identity                    provider.Identity
	saved                       string
	callbackDigest              string
	canceled                    bool
}
type providerLoginRuntime struct {
	mu    sync.Mutex
	flows map[string]*providerLoginFlow
}

func fingerprintCredentials(raw json.RawMessage) string {
	d := sha256.Sum256(raw)
	return hex.EncodeToString(d[:])
}
func (s *Server) providerPreview(f *providerLoginFlow) map[string]any {
	return map[string]any{"ready": true, "provider": f.provider, "scope": f.scope, "name": f.identity.Name, "uid": codearts.Mask(f.identity.ID), "expires_at": f.identity.ExpiresAt, "has_refresh": f.identity.HasRefresh, "preview_id": fingerprintCredentials(f.credentials), "existing": s.Store.FindProviderAccount(f.provider, f.scope, f.identity.ID) != nil}
}
func (s *Server) handleProviderLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Action    string `json:"action"`
		Provider  string `json:"provider"`
		Scope     string `json:"scope"`
		SessionID string `json:"session_id"`
		Input     string `json:"input"`
		Preview   string `json:"preview_id"`
		Replace   bool   `json:"replace_existing"`
		Name      string `json:"name"`
		MachineID string `json:"machine_id"`
		DeviceID  string `json:"device_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body) != nil {
		writeErr(w, 400, "登录参数无效", "invalid_request_error")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if body.Action == "start" {
		adapter, ok := s.Providers.Get(body.Provider)
		if !ok {
			writeErr(w, 400, "该上游尚未接入", "invalid_request_error")
			return
		}
		login, ok := adapter.(provider.BrowserLogin)
		if !ok {
			writeErr(w, 400, "该上游不支持浏览器登录", "invalid_request_error")
			return
		}
		if body.Scope == "" {
			body.Scope = "cn"
		}
		f := &providerLoginFlow{provider: body.Provider, scope: body.Scope, created: time.Now()}
		id := codearts.RandomHex(16)
		s.providerLogins.mu.Lock()
		if s.providerLogins.flows == nil {
			s.providerLogins.flows = map[string]*providerLoginFlow{}
		}
		for k, v := range s.providerLogins.flows {
			if time.Since(v.created) > 10*time.Minute {
				if adapter, ok := s.Providers.Get(v.provider); ok {
					if cleanup, ok := adapter.(provider.LoginReleaser); ok {
						cleanup.ReleaseLogin(v.state)
					}
				}
				delete(s.providerLogins.flows, k)
			}
		}
		if len(s.providerLogins.flows) >= 128 {
			s.providerLogins.mu.Unlock()
			writeErr(w, 429, "未完成的授权过多，请稍后再试", "rate_limit_error")
			return
		}
		s.providerLogins.flows[id] = f
		f.mu.Lock()
		s.providerLogins.mu.Unlock()
		defer f.mu.Unlock()
		var upstream provider.Login
		var err error
		status := http.StatusBadGateway
		errType := "upstream_error"
		if deviceLogin, ok := adapter.(provider.DeviceBrowserLogin); ok {
			upstream, err = deviceLogin.BeginLoginWithDevice(ctx, body.Scope, body.MachineID, body.DeviceID)
			status = http.StatusBadRequest
			errType = "invalid_request_error"
		} else {
			upstream, err = login.BeginLogin(ctx, body.Scope)
		}
		if err != nil {
			f.canceled = true
			writeErr(w, status, err.Error(), errType)
			return
		}
		f.state = upstream.State
		f.url = upstream.URL
		writeJSON(w, 200, map[string]any{"session_id": id, "url": f.url, "provider": f.provider, "scope": f.scope, "expires_at": f.created.Add(10 * time.Minute).Unix()})
		return
	}
	s.providerLogins.mu.Lock()
	f := s.providerLogins.flows[body.SessionID]
	s.providerLogins.mu.Unlock()
	if f == nil {
		writeErr(w, 404, "授权会话已失效，请重新获取链接", "invalid_request_error")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if body.Action == "cancel" {
		f.canceled = true
		if adapter, ok := s.Providers.Get(f.provider); ok {
			if cleanup, ok := adapter.(provider.LoginReleaser); ok {
				cleanup.ReleaseLogin(f.state)
			}
		}
		f.credentials = nil
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if f.canceled || time.Since(f.created) > 10*time.Minute {
		writeErr(w, 410, "授权已取消或过期，请重新获取链接", "invalid_request_error")
		return
	}
	switch body.Action {
	case "check":
		if f.saved != "" {
			writeErr(w, 409, "本次授权已保存，请刷新账号列表", "invalid_request_error")
			return
		}
		adapter, _ := s.Providers.Get(f.provider)
		callback, usesCallback := adapter.(provider.CallbackLogin)
		if usesCallback && f.credentials != nil && strings.TrimSpace(body.Input) != "" && fingerprintCredentials([]byte(strings.TrimSpace(body.Input))) != f.callbackDigest {
			writeErr(w, 409, "本次授权已核对其他回调；切换账号请重新生成登录链接", "invalid_request_error")
			return
		}

		if body.Input != "" && !usesCallback {
			u, e := url.Parse(strings.TrimSpace(body.Input))
			if e != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Query().Get("state") != f.state {
				writeErr(w, 400, "回调地址不属于本次授权；WorkBuddy 也可完成登录后直接点击检查", "invalid_request_error")
				return
			}
		}
		if f.credentials == nil {
			var raw json.RawMessage
			var ready bool
			var err error
			if usesCallback {
				raw, ready, err = callback.CheckCallback(ctx, f.scope, f.state, body.Input)
			} else {
				login := adapter.(provider.BrowserLogin)
				raw, ready, err = login.CheckLogin(ctx, f.scope, f.state)
			}
			if err != nil {
				writeErr(w, 502, err.Error(), "upstream_error")
				return
			}
			if !ready {
				writeJSON(w, 200, map[string]any{"ready": false, "message": "尚未完成授权；Trae 请在浏览器授权后粘贴完整回调地址，WorkBuddy 可直接检查"})
				return
			}
			normalized, identity, err := adapter.Normalize(raw)
			if err != nil || identity.Scope != f.scope {
				writeErr(w, 502, "授权身份与地区不匹配", "upstream_error")
				return
			}
			f.credentials = normalized
			if usesCallback {
				f.callbackDigest = fingerprintCredentials([]byte(strings.TrimSpace(body.Input)))
			}
			f.identity = identity
		}
		writeJSON(w, 200, s.providerPreview(f))
	case "complete":
		if f.saved != "" {
			if s.Store.Account(f.saved) == nil {
				writeErr(w, 410, "保存的账号已被删除，请重新授权", "invalid_request_error")
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "account_id": f.saved})
			return
		}
		if len(f.credentials) == 0 || body.Preview != fingerprintCredentials(f.credentials) {
			writeErr(w, 409, "请检查授权并核对账号预览", "invalid_request_error")
			return
		}
		if f.identity.ExpiresAt <= time.Now().Unix() {
			writeErr(w, 410, "授权凭据已过期", "invalid_request_error")
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = f.identity.Name
		}
		a := &codearts.Account{ID: codearts.RandomHex(8), Provider: f.provider, Scope: f.scope, ExternalID: f.identity.ID, Credentials: f.credentials, Name: name, Enabled: true, Source: "browser", Status: "ok"}
		if _, err := s.Store.UpsertAccountAndSave(a, body.Replace); err != nil {
			writeErr(w, 409, "保存失败："+err.Error(), "invalid_request_error")
			return
		}
		f.saved = a.ID
		if adapter, ok := s.Providers.Get(f.provider); ok {
			if cleanup, ok := adapter.(provider.LoginReleaser); ok {
				cleanup.ReleaseLogin(f.state)
			}
		}
		f.credentials = nil
		writeJSON(w, 200, map[string]any{"ok": true, "account_id": a.ID})
	default:
		writeErr(w, 400, "未知授权操作", "invalid_request_error")
	}
}
func (s *Server) handleProviderImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Provider    string          `json:"provider"`
		Credentials json.RawMessage `json:"credentials"`
		Name        string          `json:"name"`
		Replace     bool            `json:"replace_existing"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body) != nil {
		writeErr(w, 400, "导入格式无效", "invalid_request_error")
		return
	}
	adapter, ok := s.Providers.Get(body.Provider)
	if !ok {
		writeErr(w, 400, "该上游尚未接入", "invalid_request_error")
		return
	}
	raw, identity, err := adapter.Normalize(body.Credentials)
	if err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	if identity.ExpiresAt <= time.Now().Unix() {
		writeErr(w, 400, "导入凭据已过期，请先在原客户端刷新或重新登录", "invalid_request_error")
		return
	}
	verifier, ok := adapter.(interface {
		Verify(context.Context, json.RawMessage) (provider.Identity, error)
	})
	if !ok {
		writeErr(w, 400, "该上游尚不支持凭据身份校验", "invalid_request_error")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	verified, err := verifier.Verify(ctx, raw)
	if err != nil || verified.ID != identity.ID || verified.Scope != identity.Scope {
		writeErr(w, 400, "上游账号身份校验失败，请检查完整凭据和所属地区", "invalid_request_error")
		return
	}
	name := body.Name
	if name == "" {
		name = verified.Name
	}
	a := &codearts.Account{ID: codearts.RandomHex(8), Provider: body.Provider, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Name: name, Enabled: true, Source: "session", Status: "ok"}
	if _, err = s.Store.UpsertAccountAndSave(a, body.Replace); err != nil {
		writeErr(w, 409, fmt.Sprintf("保存失败：%v", err), "invalid_request_error")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "account_id": a.ID})
}
