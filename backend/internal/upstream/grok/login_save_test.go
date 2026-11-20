package grok

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
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
func TestBuildLoginUsesExistingSaveTransaction(t *testing.T) {
	c := New()
	tokens, catalogs := 0, 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokens++
			json.NewEncoder(w).Encode(map[string]any{"access_token": fixtureToken("user", "team"), "refresh_token": "refresh", "expires_in": 3600, "token_type": "Bearer"})
		case "/models":
			catalogs++
			io.WriteString(w, `{"data":[{"id":"grok-test"}]}`)
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	c.TokenURL = up.URL + "/token"
	c.BaseURL = up.URL
	reg := provider.NewRegistry()
	reg.Register(c)
	store := &importStore{fail: true}
	login := accounts.NewProviderLogins(store, func(int) string { return "fixture" })
	started, err := login.Apply(context.Background(), accounts.ProviderLoginRequest{Action: "start", Provider: "grok", Scope: "build"}, reg)
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
	if err != nil || saved.State != accounts.LoginSaved || tokens != 1 || catalogs != 1 || store.saved.Scope != "build" {
		t.Fatal(saved, err, tokens)
	}
}
