package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleNeverIssuesLoopbackCapabilities(t *testing.T) {
	s := poolServer(t, 0)
	for _, host := range []string{"127.0.0.1:10088", "localhost:10088", "panel.example"} {
		for _, path := range []string{"/", "/login", "/user"} {
			r := httptest.NewRequest("GET", "http://"+host+path, nil)
			r.RemoteAddr = "127.0.0.1:5555"
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 200 || strings.Contains(w.Body.String(), "local-panel-session") || len(w.Result().Cookies()) != 0 {
				t.Fatal("console issued capability", host, path, w.Code)
			}
		}
	}
}
