package bedrock

import (
	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

func TestImportChecksInferenceAndSaveFailure(t *testing.T) {
	c := New()
	valid := false
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !valid {
			return testResponse(403, "application/json", `{"__type":"AccessDeniedException"}`), nil
		}
		return testResponse(200, "application/vnd.amazon.eventstream", successFrames()), nil
	})
	reg := provider.NewRegistry()
	reg.Register(c)
	store := &importStore{fail: true}
	login := accounts.NewProviderLogins(store, func(int) string { return "fixture" })
	req := accounts.ProviderImportRequest{Provider: provider.Bedrock, Credentials: json.RawMessage(`{"aws_access_key_id":"fixture","aws_secret_access_key":"secret","model_ids":["anthropic.claude-fixture"]}`)}
	if _, err := login.Import(context.Background(), req, reg); err == nil || store.saved != nil {
		t.Fatal("invalid IAM access saved")
	}
	valid = true
	if _, err := login.Import(context.Background(), req, reg); err == nil || store.saved != nil {
		t.Fatal("false save success")
	}
	store.fail = false
	if _, err := login.Import(context.Background(), req, reg); err != nil || store.saved == nil || store.saved.Scope != "us-east-1" {
		t.Fatal(err, store.saved)
	}
}
