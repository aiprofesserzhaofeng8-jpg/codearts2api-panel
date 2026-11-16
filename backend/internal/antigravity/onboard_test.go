package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnboardingDefaultTierAndReadOnlyVerification(t *testing.T) {
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.Write([]byte(`{"id":"subject"}`))
		case "/v1internal:loadCodeAssist":
			w.Write([]byte(`{"allowedTiers":[{"id":"NOT_SELECTED"},{"id":"DEFAULT","isDefault":true}]}`))
		case "/v1internal:onboardUser":
			count++
			var body struct {
				Tier     string            `json:"tierId"`
				Metadata map[string]string `json:"metadata"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Tier != "DEFAULT" || body.Metadata["ideType"] != "ANTIGRAVITY" || body.Metadata["pluginType"] != "GEMINI" {
				t.Error("incorrect onboarding parameters")
			}
			if count == 1 {
				w.Write([]byte(`{"done":false}`))
				return
			}
			w.Write([]byte(`{"done":true,"response":{"cloudaicompanionProject":{"id":"project"}}}`))
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.BaseURL = s.URL
	c.UserInfoURL = s.URL + "/user"
	if _, err := c.Verify(context.Background(), credential(t)); err == nil || count != 0 {
		t.Fatal("import validation caused enrollment")
	}
	a := credentials{Access: "access"}
	if err := c.prepare(context.Background(), &a, true); err != nil || a.Project != "project" || count != 2 {
		t.Fatal(a.Project, err, count)
	}
}
func TestOnboardingRejectsUnknownOutcomesAndCancels(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		cancel     bool
	}{
		{"missing-done", `{}`, 200, false}, {"null-done", `{"done":null}`, 200, false}, {"missing-project", `{"done":true,"response":{}}`, 200, false}, {"error", `{"error":{"message":"private"}}`, 200, false}, {"forbidden", `{}`, 403, false}, {"overloaded", `{}`, 503, false}, {"cancel", `{"done":false}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
				if tc.cancel {
					cancel()
				}
			}))
			defer s.Close()
			t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
			t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
			c := New()
			c.BaseURL = s.URL
			a := credentials{Access: "access"}
			load := map[string]json.RawMessage{"allowedTiers": json.RawMessage(`[{"id":"DEFAULT","isDefault":true}]`)}
			err := c.onboard(ctx, &a, load)
			if err == nil || calls != 1 || a.Project != "" {
				t.Fatal("invalid outcome accepted/retried", err, calls)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
func TestOnboardingDoesNotGuessTierOrChangeProject(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"done":true,"response":{"cloudaicompanionProject":"other"}}`))
	}))
	defer s.Close()
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.BaseURL = s.URL
	for _, tiers := range []string{`null`, `[]`, `[{"id":"PAID","isDefault":false}]`, `[{"id":"","isDefault":true}]`} {
		a := credentials{Access: "access"}
		if err := c.onboard(context.Background(), &a, map[string]json.RawMessage{"allowedTiers": json.RawMessage(tiers)}); err == nil || calls != 0 {
			t.Fatal("guessed tier", err, calls)
		}
	}
	a := credentials{Access: "access", Project: "original"}
	err := c.onboard(context.Background(), &a, map[string]json.RawMessage{"allowedTiers": json.RawMessage(`[{"id":"DEFAULT","isDefault":true}]`)})
	if err == nil || a.Project != "original" || calls != 1 {
		t.Fatal("rebound project", err)
	}
}

func TestOnboardingPendingHasBoundedAttempts(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(`{"done":false}`)) }))
	defer s.Close()
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "fixture-secret")
	c := New()
	c.BaseURL = s.URL
	a := credentials{Access: "access"}
	err := c.onboard(context.Background(), &a, map[string]json.RawMessage{"allowedTiers": json.RawMessage(`[{"id":"DEFAULT","isDefault":true}]`)})
	if err == nil || calls != 5 || a.Project != "" {
		t.Fatal("pending incorrectly completed", calls, err)
	}
}
