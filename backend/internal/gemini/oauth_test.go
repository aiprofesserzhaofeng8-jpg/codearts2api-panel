package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

func cliFixture() json.RawMessage {
	b, _ := json.Marshal(cliCredentials{Access: "old-token", Refresh: "refresh-old", Subject: "user-1", Project: "project-1", Expires: time.Now().Unix() + 3600, Scope: "cli", Auth: "oauth"})
	return b
}

func TestCLIAuthScopeAndRefresh(t *testing.T) {
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	raw := cliFixture()
	_, before, err := c.Normalize(raw)
	if err != nil || before.Scope != "cli" || before.NonExpiring || !before.HasRefresh {
		t.Fatal(before, err)
	}
	_, api, _ := c.Normalize(fixtureKey)
	if before.ID == api.ID {
		t.Fatal("mode collision")
	}
	for _, bad := range []string{`{"api_key":"key","access_token":"t"}`, `{"scope":"cli","api_key":"key"}`, strings.Replace(string(raw), `"cli"`, `"api"`, 1), strings.Replace(string(raw), `"subject":"user-1"`, `"subject":""`, 1)} {
		if _, _, err := c.Normalize([]byte(bad)); err == nil {
			t.Fatal("invalid CLI accepted")
		}
	}
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/token" {
			t.Fatal("refresh performed optional I/O")
		}
		r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh-old" || r.Form.Get("client_id") != "fixture-client" {
			t.Error("bad form")
		}
		io.WriteString(w, `{"access_token":"new-token","refresh_token":"rotated","expires_in":3600,"token_type":"Bearer"}`)
	}))
	defer up.Close()
	c.TokenURL = up.URL + "/token"
	next, err := c.Refresh(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var a cliCredentials
	json.Unmarshal(next, &a)
	_, after, _ := c.Normalize(next)
	if calls != 1 || a.Refresh != "rotated" || a.Access != "new-token" || before.ID != after.ID || a.Project != "project-1" {
		t.Fatal("refresh changed identity or lost rotation")
	}
	c.TokenURL = up.URL + "/token"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Refresh(ctx, raw); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCLILoginCachedExchangeAndIdentity(t *testing.T) {
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	tokens, loads := 0, 0
	failUser := true
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokens++
			r.ParseForm()
			if r.Form.Get("code_verifier") == "" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != GeminiCLIRedirectURI {
				t.Error("invalid exchange")
			}
			io.WriteString(w, `{"access_token":"new-token","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`)
		case "/user":
			if failUser {
				w.WriteHeader(503)
				return
			}
			io.WriteString(w, `{"id":"user-1","email":"fixture@example.invalid"}`)
		case "/v1internal:loadCodeAssist":
			loads++
			io.WriteString(w, `{"cloudaicompanionProject":"project-1","currentTier":{"id":"FREE"}}`)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	c.TokenURL = up.URL + "/token"
	c.UserInfoURL = up.URL + "/user"
	c.CLIBase = up.URL
	if _, err := c.BeginLogin(context.Background(), "api"); err == nil {
		t.Fatal("API mode entered OAuth")
	}
	login, err := c.BeginLogin(context.Background(), "cli")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(login.URL)
	if u.Host != "accounts.google.com" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("state") != login.State || u.Query().Get("access_type") != "offline" {
		t.Fatal("invalid authorization")
	}
	callback := GeminiCLIRedirectURI + "?code=fixture-code&state=" + login.State
	if _, _, err := c.CheckCallback(context.Background(), "cli", login.State, strings.Replace(callback, login.State, "wrong", 1)); err == nil || tokens != 0 {
		t.Fatal("unbound callback")
	}
	if _, _, err := c.CheckCallback(context.Background(), "cli", login.State, callback); err == nil {
		t.Fatal("false successful user validation")
	}
	failUser = false
	raw, ready, err := c.CheckCallback(context.Background(), "cli", login.State, callback)
	if err != nil || !ready || tokens != 1 || loads != 1 {
		t.Fatal(tokens, loads, ready, err)
	}
	_, id, err := c.Normalize(raw)
	if err != nil || id.Scope != "cli" || !id.HasRefresh {
		t.Fatal(id, err)
	}
	c.ReleaseLogin(login.State)
	if _, _, err := c.CheckCallback(context.Background(), "cli", login.State, callback); err == nil {
		t.Fatal("canceled login resurrected")
	}
}

func TestCLIProjectOnboarding(t *testing.T) {
	for _, kind := range []string{"new", "existing", "mismatch", "load-denied", "registered-no-project", "no-default"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					io.WriteString(w, `{"id":"user-1"}`)
				case "/v1internal:loadCodeAssist":
					switch kind {
					case "load-denied":
						w.WriteHeader(403)
					case "existing":
						io.WriteString(w, `{"currentTier":"FREE","cloudaicompanionProject":"project-1"}`)
					case "mismatch":
						io.WriteString(w, `{"currentTier":"FREE","cloudaicompanionProject":"project-other"}`)
					case "registered-no-project":
						io.WriteString(w, `{"currentTier":"FREE"}`)
					case "no-default":
						io.WriteString(w, `{"allowedTiers":[{"id":"PAID","isDefault":false}]}`)
					default:
						io.WriteString(w, `{"allowedTiers":[{"id":"FREE","isDefault":true}]}`)
					}
				case "/v1internal:onboardUser":
					calls++
					var obj map[string]json.RawMessage
					json.NewDecoder(r.Body).Decode(&obj)
					if string(obj["tierId"]) != `"FREE"` {
						t.Error("wrong tier")
					}
					io.WriteString(w, `{"done":true,"response":{"cloudaicompanionProject":{"id":"project-1"}}}`)
				default:
					t.Error("unexpected I/O")
					w.WriteHeader(404)
				}
			}))
			defer up.Close()
			t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
			t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
			c := New()
			c.CLIBase = up.URL
			c.UserInfoURL = up.URL + "/user"
			a := cliCredentials{Access: "token"}
			if kind == "mismatch" {
				a.Project = "project-1"
			}
			err := c.prepare(context.Background(), &a, true)
			if kind == "new" || kind == "existing" {
				if err != nil || a.Project != "project-1" {
					t.Fatal(a, err)
				}
			} else if err == nil {
				t.Fatal("invalid project accepted")
			}
			want := 0
			if kind == "new" {
				want = 1
			}
			if calls != want {
				t.Fatal("unexpected onboarding write", calls)
			}
		})
	}
}

func TestCLIIdentityAndCatalog(t *testing.T) {
	mismatch := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			if mismatch {
				io.WriteString(w, `{"id":"other-user"}`)
			} else {
				io.WriteString(w, `{"id":"user-1"}`)
			}
			return
		}
		io.WriteString(w, `{"currentTier":"FREE","cloudaicompanionProject":"project-1"}`)
	}))
	defer up.Close()
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.CLIBase = up.URL
	c.UserInfoURL = up.URL + "/user"
	models, err := c.Models(context.Background(), cliFixture())
	if err != nil || len(models) == 0 {
		t.Fatal(err)
	}
	for _, m := range models {
		if m.Scope != "cli" || m.Context != 0 || m.ToolCalling != "" || strings.Contains(m.ID, "image") {
			t.Fatal("invented catalog capabilities", m)
		}
	}
	mismatch = true
	if _, err := c.Verify(context.Background(), cliFixture()); err == nil {
		t.Fatal("accepted wrong user")
	}
}

func TestCLITokenFailures(t *testing.T) {
	for _, body := range []string{`{}`, `{"access_token":"token","expires_in":0,"token_type":"Bearer"}`, `{"error":"invalid_grant","error_description":"secret"}`} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
		t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
		c := New()
		c.TokenURL = up.URL
		_, err := c.Refresh(context.Background(), cliFixture())
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
		up.Close()
	}
}

var _ provider.Adapter = (*Client)(nil)
var _ provider.CallbackLogin = (*Client)(nil)

func TestCLILoginUsesExistingSaveTransaction(t *testing.T) {
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	tokens := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokens++
			io.WriteString(w, `{"access_token":"token","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`)
		case "/user":
			io.WriteString(w, `{"id":"user-1"}`)
		default:
			io.WriteString(w, `{"currentTier":"FREE","cloudaicompanionProject":"project-1"}`)
		}
	}))
	defer up.Close()
	c.TokenURL = up.URL + "/token"
	c.UserInfoURL = up.URL + "/user"
	c.CLIBase = up.URL
	reg := provider.NewRegistry()
	reg.Register(c)
	store := &importStore{fail: true}
	login := accounts.NewProviderLogins(store, func(int) string { return "fixture" })
	started, err := login.Apply(context.Background(), accounts.ProviderLoginRequest{Action: "start", Provider: "gemini", Scope: "cli"}, reg)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(started.URL)
	callback := GeminiCLIRedirectURI + "?code=fixture&state=" + u.Query().Get("state")
	preview, err := login.Apply(context.Background(), accounts.ProviderLoginRequest{Action: "check", SessionID: started.SessionID, Input: callback}, reg)
	if err != nil || preview.State != accounts.LoginPreview || store.saved != nil {
		t.Fatal(preview, err)
	}
	request := accounts.ProviderLoginRequest{Action: "complete", SessionID: started.SessionID, Preview: preview.PreviewID}
	if _, err := login.Apply(context.Background(), request, reg); err == nil || store.saved != nil {
		t.Fatal("false save success")
	}
	store.fail = false
	saved, err := login.Apply(context.Background(), request, reg)
	if err != nil || saved.State != accounts.LoginSaved || tokens != 1 || store.saved.Scope != "cli" {
		t.Fatal(saved, err, tokens)
	}
}

func TestCLIOnboardingPollsReadOnlyAndCancels(t *testing.T) {
	for _, cancelPoll := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelPoll), func(t *testing.T) {
			writes, reads := 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					io.WriteString(w, `{"id":"user-1"}`)
				case "/v1internal:loadCodeAssist":
					io.WriteString(w, `{"allowedTiers":[{"id":"FREE","isDefault":true}]}`)
				case "/v1internal:onboardUser":
					writes++
					io.WriteString(w, `{"done":false,"name":"operations/fixture"}`)
					if cancelPoll {
						cancel()
					}
				case "/v1internal/operations/fixture":
					reads++
					if r.Method != "GET" {
						t.Error("poll writes")
					}
					io.WriteString(w, `{"done":true,"response":{"cloudaicompanionProject":"project-1"}}`)
				default:
					t.Error("unexpected route")
					w.WriteHeader(404)
				}
			}))
			defer up.Close()
			t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
			t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
			c := New()
			c.CLIBase = up.URL
			c.UserInfoURL = up.URL + "/user"
			a := cliCredentials{Access: "token"}
			err := c.prepare(ctx, &a, true)
			if writes != 1 {
				t.Fatal("onboard repeated", writes)
			}
			if cancelPoll {
				if !errors.Is(err, context.Canceled) || reads != 0 {
					t.Fatal(err, reads)
				}
			} else if err != nil || reads != 1 || a.Project != "project-1" {
				t.Fatal(err, reads)
			}
		})
	}
}
