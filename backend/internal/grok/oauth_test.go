package grok

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fixtureToken(subject, team string) string {
	b, _ := json.Marshal(map[string]any{"sub": subject, "team_id": team, "exp": time.Now().Unix() + 3600})
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(b) + ".fixture"
}
func fixture(t *testing.T) json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(credentials{Access: fixtureToken("user", "team"), Refresh: "old-refresh", Expires: time.Now().Unix() + 7200, Scope: "build"})
	return b
}
func TestLoginPKCEStateNonceAndRedemption(t *testing.T) {
	c := New()
	ctx := context.Background()
	if _, err := c.BeginLogin(ctx, "global"); err == nil {
		t.Fatal("wrong scope accepted")
	}
	login, err := c.BeginLogin(ctx, "build")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(login.URL)
	q := u.Query()
	if u.Host != "auth.x.ai" || q.Get("client_id") != ClientID || q.Get("redirect_uri") != RedirectURI || q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("plan") != "generic" {
		t.Fatal("wrong auth parameters")
	}
	other, _ := c.BeginLogin(ctx, "build")
	otherURL, _ := url.Parse(other.URL)
	if otherURL.Query().Get("nonce") == q.Get("nonce") {
		t.Fatal("reused nonce")
	}
	c.ReleaseLogin(other.State)
	calls, catalogs := 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			catalogs++
			if r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" || r.Header.Get("x-grok-client-version") != cliVersion {
				t.Error("missing CLI headers")
			}
			if catalogs == 1 {
				w.WriteHeader(503)
				return
			}
			w.Write([]byte(`{"data":[{"id":"grok-test"}]}`))
			return
		}
		calls++
		r.ParseForm()
		if r.Form.Get("code_verifier") == "" || r.Form.Get("client_id") != ClientID || r.Form.Get("redirect_uri") != RedirectURI || r.Form.Get("grant_type") != "authorization_code" {
			t.Error("wrong exchange")
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": fixtureToken("user", "team"), "refresh_token": "refresh", "expires_in": 3600, "token_type": "Bearer"})
	}))
	defer s.Close()
	c.TokenURL = s.URL + "/token"
	c.BaseURL = s.URL
	if _, _, err = c.CheckCallback(ctx, "build", login.State, "code#wrong"); err == nil || calls != 0 {
		t.Fatal("state check bypassed")
	}
	callback := RedirectURI + "?code=fixture&state=" + url.QueryEscape(login.State)
	if _, ready, err := c.CheckCallback(ctx, "build", login.State, callback); err == nil || ready {
		t.Fatal("catalog failure hidden")
	}
	for i := 0; i < 2; i++ {
		raw, ready, err := c.CheckCallback(ctx, "build", login.State, callback)
		if err != nil || !ready {
			t.Fatal(err)
		}
		_, id, err := c.Normalize(raw)
		if err != nil || id.Scope != "build" || !id.HasRefresh {
			t.Fatal(id, err)
		}
	}
	if calls != 1 {
		t.Fatal("repeated code redemption")
	}
	c.ReleaseLogin(login.State)
	if _, _, err = c.CheckCallback(ctx, "build", login.State, callback); err == nil {
		t.Fatal("released login accepted")
	}
}
func TestRefreshRotationAndIdentity(t *testing.T) {
	subject, team := "user", "team"
	rotated := true
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
			t.Error("wrong refresh")
		}
		out := map[string]any{"access_token": fixtureToken(subject, team), "expires_in": 1800, "token_type": "Bearer"}
		if rotated {
			out["refresh_token"] = "new-refresh"
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer s.Close()
	c := New()
	c.TokenURL = s.URL
	raw := fixture(t)
	_, before, _ := c.Normalize(raw)
	for _, rotation := range []bool{true, false} {
		rotated = rotation
		b, err := c.Refresh(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		_, after, err := c.Normalize(b)
		var a credentials
		json.Unmarshal(b, &a)
		want := "old-refresh"
		if rotation {
			want = "new-refresh"
		}
		if err != nil || before.ID != after.ID || a.Refresh != want {
			t.Fatal("lost identity/rotation", err)
		}
	}
	subject = "other"
	if _, err := c.Refresh(context.Background(), raw); err == nil {
		t.Fatal("subject changed")
	}
	subject = "user"
	team = "other"
	if _, err := c.Refresh(context.Background(), raw); err == nil {
		t.Fatal("team changed")
	}
	if calls != 4 {
		t.Fatal("unexpected retry")
	}
}
func TestNormalizeDoesNotTrustStoredIdentityOrExtendExpiry(t *testing.T) {
	c := New()
	raw := fixture(t)
	b, id, err := c.Normalize(raw)
	if err != nil || id.ExpiresAt > time.Now().Unix()+3600 {
		t.Fatal(id, err)
	}
	var obj map[string]any
	json.Unmarshal(b, &obj)
	for key, value := range map[string]any{"scope": "api", "subject": "other", "team_id": "other", "base_url": "http://localhost/private", "api_key": "key", "password": "password", "sso_token": "sso", "access_token": "opaque"} {
		copy := map[string]any{}
		for k, v := range obj {
			copy[k] = v
		}
		copy[key] = value
		bad, _ := json.Marshal(copy)
		if _, _, err = c.Normalize(bad); err == nil {
			t.Fatal("accepted", key)
		}
	}
	// Normalization alone deliberately does not prove the JWT signature. The
	// synthetic fixture is accepted as metadata and must still be checked upstream.
}
func TestExchangeErrorsAreBoundedAndRedacted(t *testing.T) {
	for _, status := range []int{401, 403, 429, 503, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "http://127.0.0.1:1/private")
				w.WriteHeader(status)
				w.Write([]byte(`{"error":"private-token"}`))
			}))
			defer s.Close()
			c := New()
			c.TokenURL = s.URL
			_, err := c.Refresh(context.Background(), fixture(t))
			if err == nil || strings.Contains(err.Error(), "private-token") || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
	c := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.BeginLogin(ctx, "build"); err == nil {
		t.Fatal("canceled login started")
	}
}

func TestCatalogValidationDoesNotInventFallback(t *testing.T) {
	body := `{"data":[{"id":"grok-test","context_window":123,"max_output_tokens":12},{"id":"grok-test"},{"id":"grok-imagine-image"}]}`
	status := 200
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); w.Write([]byte(body)) }))
	defer s.Close()
	c := New()
	c.BaseURL = s.URL
	models, err := c.Models(context.Background(), fixture(t))
	if err != nil || len(models) != 1 || models[0].ID != "grok-test" || models[0].Context != 123 || models[0].ImageInput != "" {
		t.Fatal(models, err)
	}
	for _, bad := range []string{`{}`, `{"data":null}`, `{"data":[{"id":"grok-test","context_window":-1}]}`} {
		body = bad
		if _, err = c.Models(context.Background(), fixture(t)); err == nil {
			t.Fatal("accepted bad catalog")
		}
	}
	body = `{"data":[]}`
	if _, err = c.Verify(context.Background(), fixture(t)); err == nil {
		t.Fatal("empty catalog verified")
	}
	body = `{"data":[{"id":"grok-test"}]}`
	for _, code := range []int{401, 403, 404, 429, 503} {
		status = code
		if _, err = c.Verify(context.Background(), fixture(t)); err == nil {
			t.Fatal("invented fallback", code)
		}
	}
}
