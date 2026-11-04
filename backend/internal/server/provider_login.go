package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
)

// Domain errors retain the existing admin API status and error envelope.
func writeLoginError(w http.ResponseWriter, err error) {
	status, kind := http.StatusBadGateway, "upstream_error"
	var failure *accounts.LoginError
	if errors.As(err, &failure) {
		kind = "invalid_request_error"
		switch failure.Kind {
		case accounts.LoginInvalid:
			status = http.StatusBadRequest
		case accounts.LoginMissing:
			status = http.StatusNotFound
		case accounts.LoginExpired:
			status = http.StatusGone
		case accounts.LoginConflict:
			status = http.StatusConflict
		case accounts.LoginCapacity:
			status, kind = http.StatusTooManyRequests, "rate_limit_error"
		case accounts.LoginInternal:
			status, kind = http.StatusInternalServerError, "server_error"
		case accounts.LoginUpstream:
			kind = "upstream_error"
		}
	}
	writeErr(w, status, err.Error(), kind)
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
	result, err := s.ProviderLogins.Apply(r.Context(), accounts.ProviderLoginRequest{
		Action: body.Action, Provider: body.Provider, Scope: body.Scope, SessionID: body.SessionID,
		Input: body.Input, Preview: body.Preview, Replace: body.Replace, Name: body.Name,
		MachineID: body.MachineID, DeviceID: body.DeviceID,
	}, s.Providers)
	if err != nil {
		writeLoginError(w, err)
		return
	}
	switch result.State {
	case accounts.LoginStarted:
		writeJSON(w, 200, map[string]any{"session_id": result.SessionID, "url": result.URL, "provider": result.Provider, "scope": result.Scope, "expires_at": result.ExpiresAt})
	case accounts.LoginPending:
		writeJSON(w, 200, map[string]any{"ready": false, "message": "尚未完成授权；Trae 请在浏览器授权后粘贴完整回调地址，WorkBuddy / Qoder / Kiro 可直接检查"})
	case accounts.LoginPreview:
		writeJSON(w, 200, map[string]any{"ready": true, "provider": result.Provider, "scope": result.Scope, "name": result.Identity.Name, "uid": codearts.Mask(result.Identity.ID), "expires_at": result.Identity.ExpiresAt, "has_refresh": result.Identity.HasRefresh, "preview_id": result.PreviewID, "existing": result.Existing})
	case accounts.LoginSaved:
		writeJSON(w, 200, map[string]any{"ok": true, "account_id": result.AccountID})
	case accounts.LoginCanceled:
		writeJSON(w, 200, map[string]any{"ok": true})
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
	id, err := s.ProviderLogins.Import(r.Context(), accounts.ProviderImportRequest{
		Provider: body.Provider, Credentials: body.Credentials, Name: body.Name, Replace: body.Replace,
	}, s.Providers)
	if err != nil {
		writeLoginError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "account_id": id})
}
