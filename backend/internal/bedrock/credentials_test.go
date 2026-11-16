package bedrock

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
)

func TestTemporaryCredentialsReachSignedInferenceUntilExpiry(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid-last-minute", true: "expired"}[expired], func(t *testing.T) {
			ttl := time.Minute
			if expired {
				ttl = -time.Minute
			}
			raw, _ := json.Marshal(map[string]any{"aws_access_key_id": "fixture", "aws_secret_access_key": "fixture-secret", "aws_session_token": "fixture-session", "expires_at": time.Now().Add(ttl).Unix(), "aws_region": "us-east-1", "model_ids": []string{"anthropic.claude-fixture"}})
			c := New()
			raw, identity, err := c.Normalize(raw)
			if err != nil {
				t.Fatal(err)
			}
			st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = st.UpsertAccountAndSave(&accounts.Account{ID: "a", Provider: provider.Bedrock, Scope: identity.Scope, ExternalID: identity.ID, Credentials: raw, Enabled: true}, false); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(st.Path())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("X-Amz-Security-Token") != "fixture-session" || r.Header.Get("Authorization") == "" {
					t.Error("temporary token was not signed")
				}
				return testResponse(200, "application/vnd.amazon.eventstream", successFrames()), nil
			})
			runtime := accounts.NewProviderCredentials(st, func() bool { t.Error("nonrenewable token queried refresh policy"); return true })
			resolved, err := runtime.Resolve(t.Context(), "a", c)
			if expired {
				if err == nil {
					t.Fatal("expired credentials accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = c.Verify(t.Context(), resolved); err != nil {
					t.Fatal(err)
				}
			}
			wantCalls := 1
			if expired {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("inference calls=%d want=%d", calls, wantCalls)
			}
			after, err := os.ReadFile(st.Path())
			if err != nil || !bytes.Equal(before, after) || !bytes.Equal(raw, st.Account("a").Credentials) {
				t.Fatal("resolution mutated credentials", err)
			}
		})
	}
}
