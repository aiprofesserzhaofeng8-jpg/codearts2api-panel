package antigravity

import (
	"codearts2api/internal/accounts"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func credential(t *testing.T) json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(credentials{Access: "access", Refresh: "refresh", Subject: "subject", Project: "project", Scope: "global", Auth: "oauth", Expires: time.Now().Unix() + 3600})
	return b
}
func TestOAuthRedemptionRetryAndIdentity(t *testing.T) {
	exchanges, checks := 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			exchanges++
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "fixture-client" || r.Form.Get("code_verifier") == "" || r.Form.Get("redirect_uri") != RedirectURI {
				t.Error("wrong OAuth client or PKCE")
			}
			w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`))
		case "/user":
			w.Write([]byte(`{"id":"subject","email":"test@example.invalid"}`))
		case "/v1internal:loadCodeAssist":
			checks++
			if checks == 1 {
				w.WriteHeader(503)
				return
			}
			var body map[string]map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["metadata"]["ideType"] != "ANTIGRAVITY" || r.Header.Get("User-Agent") != UserAgent {
				t.Error("wrong platform identity")
			}
			w.Write([]byte(`{"cloudaicompanionProject":"project"}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.BaseURL = s.URL
	c.TokenURL = s.URL + "/token"
	c.UserInfoURL = s.URL + "/user"
	ctx := context.Background()
	login, err := c.BeginLogin(ctx, "global")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(login.URL)
	if u.Query().Get("client_id") != "fixture-client" || u.Query().Get("code_challenge") == "" {
		t.Fatal("authorization mismatch")
	}
	if _, _, err = c.CheckCallback(ctx, "global", login.State, "code#wrong"); err == nil || exchanges != 0 {
		t.Fatal("wrong state redeemed")
	}
	input := RedirectURI + "?code=code&state=" + url.QueryEscape(login.State)
	if _, _, err = c.CheckCallback(ctx, "global", login.State, input); err == nil {
		t.Fatal("load failure hidden")
	}
	raw, ready, err := c.CheckCallback(ctx, "global", login.State, input)
	if err != nil || !ready || exchanges != 1 {
		t.Fatalf("cached redemption failed %v %d", err, exchanges)
	}
	_, id, err := c.Normalize(raw)
	if err != nil || !id.HasRefresh || id.Scope != "global" {
		t.Fatal(id, err)
	}
	var a credentials
	json.Unmarshal(raw, &a)
	a.Subject = "other"
	bad, _ := json.Marshal(a)
	if _, err = c.Verify(ctx, bad); err == nil {
		t.Fatal("identity mismatch accepted")
	}
	c.ReleaseLogin(login.State)
	if _, _, err = c.CheckCallback(ctx, "global", login.State, input); err == nil {
		t.Fatal("released session accepted")
	}
}
func TestRefreshPreservesIdentityAndRotatedToken(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh" {
			t.Error("wrong refresh")
		}
		w.Write([]byte(`{"access_token":"new","refresh_token":"rotated","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer s.Close()
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.TokenURL = s.URL
	raw := credential(t)
	_, before, _ := c.Normalize(raw)
	updated, err := c.Refresh(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	_, after, err := c.Normalize(updated)
	var a credentials
	json.Unmarshal(updated, &a)
	if err != nil || before.ID != after.ID || a.Refresh != "rotated" || a.Access != "new" || calls != 1 {
		t.Fatal("rotation not preserved", err)
	}
	for _, patch := range []string{`"scope":"cli"`, `"api_key":"mixed"`, `"base_url":"https://untrusted.invalid"`} {
		var m map[string]json.RawMessage
		json.Unmarshal(raw, &m)
		var p map[string]json.RawMessage
		json.Unmarshal([]byte("{"+patch+"}"), &p)
		for k, v := range p {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		if _, _, err = c.Normalize(b); err == nil {
			t.Fatalf("accepted %s", patch)
		}
	}
}
func TestCatalogAndQuota(t *testing.T) {
	response := `{"models":{"gemini-test":{"displayName":"Gemini test","supportsImages":true,"maxTokens":10000,"maxOutputTokens":1000,"quotaInfo":{"remainingFraction":0,"resetTime":"2026-11-15T10:00:00Z"}},"claude-test":{"quotaInfo":{}},"image-test":{}}}`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:fetchAvailableModels" || r.Header.Get("Authorization") != "Bearer access" {
			t.Error("wrong catalog endpoint")
		}
		var b FetchAvailableModelsRequest
		json.NewDecoder(r.Body).Decode(&b)
		if b.Project != "project" {
			t.Error("missing project")
		}
		w.Write([]byte(response))
	}))
	defer s.Close()
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.BaseURL = s.URL
	ctx := context.Background()
	raw := credential(t)
	models, err := c.Models(ctx, raw)
	if err != nil || len(models) != 2 || models[1].ID != "gemini-test" || models[1].Context != 10000 || models[1].ImageInput != "supported" || models[0].ImageInput != "" {
		t.Fatalf("models %#v %v", models, err)
	}
	b, err := c.Credit(ctx, raw)
	var quota accounts.CreditInfo
	json.Unmarshal(b, &quota)
	if err != nil || !quota.Available || len(quota.Windows) != 1 || quota.Windows[0].Utilization == nil || *quota.Windows[0].Utilization != 100 || quota.Windows[0].Total != nil {
		t.Fatalf("quota %s %v", b, err)
	}
	response = `{"models":{"unknown":{"quotaInfo":{}}}}`
	b, err = c.Credit(ctx, raw)
	json.Unmarshal(b, &quota)
	if err != nil || quota.Available {
		t.Fatal("missing quota converted to zero")
	}
	for _, value := range []string{"-1", "1.1", `"NaN"`} {
		response = `{"models":{"x":{"quotaInfo":{"remainingFraction":` + value + `}}}}`
		if _, err = c.Credit(ctx, raw); err == nil {
			t.Fatalf("invalid quota %s accepted", value)
		}
	}
	response = `{"models":{"x":null}}`
	if _, err = c.Models(ctx, raw); err == nil {
		t.Fatal("null model accepted")
	}
}
func TestHTTPErrorAndRedirect(t *testing.T) {
	for _, status := range []int{401, 403, 429, 503, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "http://127.0.0.1:1/leak")
				w.WriteHeader(status)
				w.Write([]byte("secret-response"))
			}))
			defer s.Close()
			t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
			t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
			c := New()
			c.BaseURL = s.URL
			_, err := c.Models(context.Background(), credential(t))
			if err == nil || strings.Contains(err.Error(), "secret-response") || calls != 1 {
				t.Fatal("error hidden, leaked or retried", err, calls)
			}
		})
	}
}
