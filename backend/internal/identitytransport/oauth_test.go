package identitytransport

import (
	"codearts2api/internal/access"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u, _ := url.Parse(r.base)
	copy.URL.Scheme = u.Scheme
	copy.URL.Host = u.Host
	return r.inner.RoundTrip(copy)
}
func TestOAuthTransportTokenExchangeAndStableUserinfo(t *testing.T) {
	for _, provider := range []string{"github", "linuxdo"} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method == "POST" {
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.Form.Get("client_secret") != "private" || r.Form.Get("code_verifier") != "verifier" || r.Form.Get("redirect_uri") != "https://panel.example/user/auth/oauth/callback" {
						t.Error("bad exchange", r.Form)
					}
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"access_token":"transient"}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer transient" {
					t.Error("missing userinfo auth")
				}
				io.WriteString(w, `{"id":9007199254740993123,"email":"never-used@example.com"}`)
			}))
			defer up.Close()
			original := http.DefaultTransport
			http.DefaultTransport = rewriteTransport{up.URL, original}
			defer func() { http.DefaultTransport = original }()
			cfg := access.IdentitySettings{GitHub: access.OAuthSettings{ClientSecret: "private"}, LinuxDO: access.OAuthSettings{ClientSecret: "private"}}
			subject, err := (&Client{}).Exchange(context.Background(), cfg, provider, "code", "verifier", "https://panel.example")
			if err != nil || subject != "9007199254740993123" || calls != 2 {
				t.Fatal(subject, err, calls)
			}
		})
	}
}
func TestOAuthTransportRejectsRedirectAndMissingIdentity(t *testing.T) {
	for _, mode := range []string{"redirect", "missing"} {
		t.Run(mode, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, "https://unexpected.example", 302)
					return
				}
				if r.Method == "POST" {
					io.WriteString(w, `{"access_token":"one"}`)
				} else {
					io.WriteString(w, `{"email":"do-not-link@example.com"}`)
				}
			}))
			defer up.Close()
			original := http.DefaultTransport
			http.DefaultTransport = rewriteTransport{up.URL, original}
			defer func() { http.DefaultTransport = original }()
			_, err := (&Client{}).Exchange(context.Background(), access.IdentitySettings{}, "github", "code", "verifier", "https://panel.example")
			if err == nil {
				t.Fatal("bad identity accepted", mode)
			}
			if strings.Contains(err.Error(), "one") {
				t.Fatal("token leaked")
			}
		})
	}
}
