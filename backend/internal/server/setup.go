package server

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"codearts2api/internal/access"
)

func setupStatus(w http.ResponseWriter, r *http.Request, pending, ready bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	if !sessionSameOrigin(r) {
		writeErr(w, 403, "跨站请求被拒绝", "permission_error")
		return
	}
	writeJSON(w, 200, map[string]any{"needs_setup": pending, "ready": ready})
}

func installedSetupStatus(w http.ResponseWriter, r *http.Request) { setupStatus(w, r, false, true) }

func closedSetupInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeErr(w, 403, access.ErrInstallationClosed.Error(), "permission_error")
}

// NewSetupHandler deliberately has no normal application routes. Before the
// first commit it cannot log in, import accounts, issue sessions or run models.
func NewSetupHandler(installation *access.Installation, completed func()) http.Handler {
	mux := http.NewServeMux()
	var once sync.Once
	var failures adminAttempts
	mux.HandleFunc("/setup/status", func(w http.ResponseWriter, r *http.Request) { setupStatus(w, r, installation.Pending(), false) })
	mux.HandleFunc("/setup/install", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !installation.Pending() {
			closedSetupInstall(w, r)
			return
		}
		if r.Method != http.MethodPost {
			writeErr(w, 405, "仅支持 POST", "invalid_request_error")
			return
		}
		if !sessionSameOrigin(r) {
			writeErr(w, 403, "跨站初始化请求被拒绝", "permission_error")
			return
		}
		if failures.blocked(r.RemoteAddr) {
			w.Header().Set("Retry-After", "60")
			writeErr(w, 429, "初始化尝试过多，请稍后再试", "rate_limit_error")
			return
		}
		var body struct {
			Email        string `json:"email"`
			Password     string `json:"password"`
			Confirmation string `json:"confirm_password"`
		}
		if !decodeUserBody(w, r, &body) {
			return
		}
		err := installation.Install(r.Context(), body.Email, body.Password, body.Confirmation)
		if err != nil {
			switch {
			case errors.Is(err, access.ErrInstallationClosed):
				closedSetupInstall(w, r)
			case errors.Is(err, access.ErrInstallationBusy):
				w.Header().Set("Retry-After", "2")
				writeErr(w, 429, err.Error(), "rate_limit_error")
			default:
				failures.failure(r.RemoteAddr)
				writeErr(w, 400, "初始安装未完成："+err.Error(), "invalid_request_error")
			}
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "admin_username": strings.ToLower(strings.TrimSpace(body.Email))})
		// Shutdown waits for this handler to return before normal startup.
		once.Do(completed)
	})
	for _, path := range []string{"/assets/", "/theme.js", "/favicon.svg", "/favicon.ico"} {
		mux.HandleFunc(path, serveConsoleAsset)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && (r.URL.Path == "/" || r.URL.Path == "/setup" || r.URL.Path == "/login" || r.URL.Path == "/user") {
			serveConsole(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeErr(w, 503, "请先完成管理员初始安装", "setup_required")
	})
	return mux
}
