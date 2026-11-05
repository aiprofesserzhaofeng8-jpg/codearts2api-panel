package server

import (
	"codearts2api/internal/webui"
	"net/http"
	"strings"
)

// Only generated production assets are embedded; frontend/src is the source.
var consoleHTML = webui.Index()
var frontendBuildID = webui.BuildID()

// consoleCSP 是控制台页面与静态资源共用的 CSP。
//
// default-src 'none' 阻止一切外部加载；脚本与样式只允许同源；
// connect-src permits same-origin and the authenticated loopback login connector;
// form-action / base-uri /
// frame-ancestors 分别阻断表单外发、<base> 劫持与点击劫持 ——
// 即使将来引入注入点，数据也运不出去、脚本也执行不了。
const consoleCSP = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"connect-src 'self' http://127.0.0.1:18765; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// consoleHeaders 统一下发控制台资源的安全响应头。
func consoleHeaders(h http.Header, contentType string) {
	h.Set("Content-Type", contentType)
	h.Set("X-Frontend-Build", frontendBuildID)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", consoleCSP)
}

// serveConsole 输出控制台单页。
func serveConsole(w http.ResponseWriter, r *http.Request) {
	consoleHeaders(w.Header(), "text/html; charset=utf-8")
	// One SPA serves both roles; validated consumer raster avatars also work
	// after automatic role routing from /login or /.

	_, _ = w.Write(consoleHTML)
}

func serveConsoleAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	data, ok := webui.Read(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	consoleHeaders(w.Header(), webui.ContentType(name))
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
