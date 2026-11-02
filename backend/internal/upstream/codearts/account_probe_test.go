package codearts

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"codearts2api/internal/accounts"
	legacy "codearts2api/internal/codearts"
)

func TestAccountProbePreservesNativePayloadAndSessionHintsOnFailure(t *testing.T) {
	for _, outcome := range []string{"success", "model-missing", "upstream-failed", "no-session"} {
		t.Run(outcome, func(t *testing.T) {
			client := legacy.NewClient()
			calls, resolves := 0, 0
			client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["model"] != "physical" || body["max_tokens"] != float64(8) || body["stream"] != false || !strings.Contains(r.Header.Get("Authorization"), "fixture-ak") {
					t.Fatal("native probe payload/signing changed", body)
				}
				reply := `{"choices":[{"message":{"content":"hi"}}]}`
				if outcome == "upstream-failed" {
					reply = `{"error_code":"4005","error_msg":"model rejected"}`
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
			})}
			session := &legacy.Session{AccessKey: "fixture-ak", SecretKey: "fixture-sk", CredentialKind: legacy.CredentialKindAKSK}
			account := &accounts.Account{ID: "fixture", Source: "manual", NativeCredential: session}
			if outcome == "no-session" {
				account.NativeCredential = nil
			}
			manager := legacy.NewManager(client)
			info, err := New(client).ProbeAccount(t.Context(), manager, account, legacy.ResolveOptions{}, func() (string, error) {
				resolves++
				if calls != 0 {
					t.Fatal("upstream called before model resolution")
				}
				if outcome == "model-missing" {
					return "", errors.New("model missing")
				}
				return "physical", nil
			})
			wantCalls := 1
			if outcome == "model-missing" || outcome == "no-session" {
				wantCalls = 0
			}
			if calls != wantCalls || (err == nil) != (outcome == "success") {
				t.Fatal("changed native error/call behavior", calls, err)
			}
			if outcome == "no-session" {
				if info != nil || resolves != 0 {
					t.Fatal("missing session still resolved model", info, resolves)
				}
			} else if info == nil || info.ExpiresAt != session.ExpiresAt.Unix() || info.AccessKey != legacy.Mask(session.AccessKey) || resolves != 1 {
				t.Fatal("lost session hints", info, resolves)
			}
		})
	}
}
