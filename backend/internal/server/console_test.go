package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"codearts2api/internal/webui"
)

// Source syntax, DOM IDs, tabs, XSS sinks and component lifetimes are checked by
// frontend typecheck/tests and the browser suite. Go checks the shipped assets.
func TestConsoleProductionAssetsAndStrictCSP(t *testing.T) {
	names := []string{"index.html", "theme.js", "favicon.svg", "favicon.ico"}
	for _, m := range regexp.MustCompile(`(?:src|href)="(/assets/[^\"]+)"`).FindAllStringSubmatch(string(consoleHTML), -1) {
		names = append(names, strings.TrimPrefix(m[1], "/"))
	}
	if len(names) < 6 {
		t.Fatal("missing production JS/CSS references")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/"+name, nil)
			if name == "index.html" {
				serveConsole(w, r)
			} else {
				serveConsoleAsset(w, r)
			}
			if w.Code != 200 || w.Body.Len() == 0 {
				t.Fatal(w.Code, "empty production asset")
			}
			for _, directive := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "connect-src 'self'", "base-uri 'none'", "form-action 'none'", "frame-ancestors 'none'"} {
				if !strings.Contains(w.Header().Get("Content-Security-Policy"), directive) {
					t.Fatal("missing CSP", directive)
				}
			}
			if strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-") {
				t.Fatal("weakened CSP")
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("X-Frontend-Build") != frontendBuildID {
				t.Fatal("invalid asset headers")
			}
		})
	}
}
func TestConsoleHTMLHasNoInlineCode(t *testing.T) {
	html := string(consoleHTML)
	for _, pattern := range []string{`<style\b`, `<script\b[^>]*>\s*[^<\s]`, `\sstyle=`, `\son\w+=`, `<base\b`, `(?:src|href)="(?:https?:)?//`} {
		if regexp.MustCompile(pattern).MatchString(html) {
			t.Fatal("unsafe production HTML", pattern)
		}
	}
	if strings.Contains(html, `name="local-panel-session"`) || frontendBuildID == "" {
		t.Fatal("unexpected local capability or missing build metadata")
	}
}
func TestConsoleAssetIsolation(t *testing.T) {
	for _, name := range []string{"../state.json", "assets/../../state.json", ".vite/manifest.json", "build.json", "assets/.secret", "/etc/passwd", "assets/../index.html", "missing.js"} {
		if _, ok := webui.Read(name); ok {
			t.Fatal("unexpected embedded resource", name)
		}
	}
	w := httptest.NewRecorder()
	serveConsoleAsset(w, httptest.NewRequest("GET", "/assets/missing.js", nil))
	if w.Code != http.StatusNotFound {
		t.Fatal(w.Code)
	}
}
