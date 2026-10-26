package codearts

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCredentialRequestsDoNotFollowRedirects(t *testing.T) {
	for _, status := range []int{302, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(200)
			}))
			defer receiver.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, strings.Replace(receiver.URL, "127.0.0.1", "localhost", 1), status)
			}))
			defer origin.Close()
			c := NewClient()
			sess := &Session{AccessKey: "canary-access", SecretKey: "canary-secret", SecurityToken: "canary-security-token"}
			resp, err := c.do(t.Context(), sess, "POST", origin.URL, "/test", []byte(`{"input":"test"}`), map[string]string{"content-type": "application/json"})
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if calls.Load() != 0 {
				t.Fatal("signed request crossed redirect boundary")
			}
			req, _ := http.NewRequest("POST", origin.URL, strings.NewReader("refresh_token=canary-refresh"))
			req.Header.Set("DPoP", "canary-proof")
			resp, err = c.HTTP.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if calls.Load() != 0 {
				t.Fatal("OAuth body crossed redirect boundary")
			}
		})
	}
}

func TestUpstreamErrorRedactsCredentialFields(t *testing.T) {
	for _, raw := range []string{`{"credentials":{"access_key_id":"canary-ak","secret_access_key":"canary-sk","security_token":"canary-st"},"refresh_token":"canary-refresh"}`, `bad refresh_token=canary-refresh secret_key=canary-sk`, `{"secret_access_key":"canary-sk`} {
		safe := RedactSecrets(raw)
		for _, secret := range []string{"canary-ak", "canary-sk", "canary-st", "canary-refresh"} {
			if strings.Contains(safe, secret) {
				t.Fatal("error still contains secret")
			}
		}
	}
	text := sessionErrorText("raw echoed token: canary-refresh", &Session{RefreshToken: "canary-refresh"})
	if strings.Contains(text, "canary-refresh") {
		t.Fatal("known value not scrubbed")
	}
}
