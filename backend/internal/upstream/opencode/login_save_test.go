package opencode

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestImportDoesNotSaveOnInvalidKeyOrDiskFailure(t *testing.T) {
	for _, scope := range []string{"zen", "go"} {
		t.Run(scope, func(t *testing.T) {
			valid := false
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					io.WriteString(w, `{"data":[{"id":"minimax-m3.1-flash","type":"chat","api":{"chat-completions":{}}}]}`)
					return
				}
				if !valid {
					w.WriteHeader(403)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, chatOK())
			}))
			defer up.Close()
			c := New()
			c.Origin = up.URL
			reg := provider.NewRegistry()
			if err := reg.Register(c); err != nil {
				t.Fatal(err)
			}
			store := &importStore{fail: true}
			login := accounts.NewProviderLogins(store, func(int) string { return "fixture" })
			request := accounts.ProviderImportRequest{Provider: "opencode", Credentials: rawKey(scope)}
			if _, err := login.Import(context.Background(), request, reg); err == nil || store.saved != nil {
				t.Fatal("invalid key saved")
			}
			valid = true
			if _, err := login.Import(context.Background(), request, reg); err == nil || store.saved != nil {
				t.Fatal("false save success")
			}
			store.fail = false
			if _, err := login.Import(context.Background(), request, reg); err != nil || store.saved == nil || store.saved.Scope != scope {
				t.Fatal(err, store.saved)
			}
		})
	}
}
