package antigravity

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type importStore struct {
	saved *accounts.Account
	fail  bool
}

func (s *importStore) Account(string) *accounts.Account                             { return s.saved }
func (s *importStore) FindProviderAccount(string, string, string) *accounts.Account { return s.saved }
func (s *importStore) UpsertAccountAndSave(a *accounts.Account, _ bool) (bool, error) {
	if s.fail {
		return false, errors.New("fixture disk failure")
	}
	s.saved = a
	return true, nil
}
func TestOnboardingUsesExistingSaveTransaction(t *testing.T) {
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	tokens, enrollments := 0, 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokens++
			io.WriteString(w, `{"access_token":"token","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`)
		case "/user":
			io.WriteString(w, `{"id":"user-1"}`)
		case "/v1internal:loadCodeAssist":
			io.WriteString(w, `{"allowedTiers":[{"id":"FREE","isDefault":true}]}`)
		case "/v1internal:onboardUser":
			enrollments++
			io.WriteString(w, `{"done":true,"response":{"cloudaicompanionProject":"project-1"}}`)
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	c.TokenURL = up.URL + "/token"
	c.UserInfoURL = up.URL + "/user"
	c.BaseURL = up.URL
	reg := provider.NewRegistry()
	reg.Register(c)
	store := &importStore{fail: true}
	login := accounts.NewProviderLogins(store, func(int) string { return "fixture" })
	started, err := login.Apply(context.Background(), accounts.ProviderLoginRequest{Action: "start", Provider: "antigravity", Scope: "global"}, reg)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(started.URL)
	callback := RedirectURI + "?code=fixture&state=" + u.Query().Get("state")
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
	if err != nil || saved.State != accounts.LoginSaved || tokens != 1 || enrollments != 1 || store.saved.Scope != "global" {
		t.Fatal(saved, err, tokens)
	}
}
