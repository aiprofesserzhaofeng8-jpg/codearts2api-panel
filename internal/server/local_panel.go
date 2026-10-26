package server

import (
	"bytes"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const localPanelHeader = "X-Local-Panel-Session"

// This capability is only embedded in a direct loopback panel response. A remote
// client or proxied request cannot obtain or reuse it; the administrator password
// remains required for those requests. No administrator password is sent to JS.
func (s *Server) localPanelRequest(r *http.Request) bool {
	if !s.AllowLocalAdmin || !s.localDirect(r) {
		return false
	}
	u, err := url.Parse("http://" + r.Host)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !isLoopbackHost(u.Hostname()) {
		return false
	}
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		_, port, err := net.SplitHostPort(addr.String())
		if err != nil || u.Port() != port {
			return false
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if origin := r.Header.Get("Origin"); origin != "" && !strings.EqualFold(origin, scheme+"://"+r.Host) {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	return true
}

func (s *Server) validLocalPanelSession(r *http.Request) bool {
	return s.localPanelToken != "" && s.localPanelRequest(r) && secureEqual(r.Header.Get(localPanelHeader), s.localPanelToken)
}

func (s *Server) servePanel(w http.ResponseWriter, r *http.Request) {
	token := ""
	if s.localPanelRequest(r) {
		token = s.localPanelToken
	}
	consoleHeaders(w.Header(), "text/html; charset=utf-8")
	page := bytes.Replace(consoleHTML, []byte(`name="local-panel-session" content=""`), []byte(`name="local-panel-session" content="`+html.EscapeString(token)+`"`), 1)
	_, _ = w.Write(page)
}
