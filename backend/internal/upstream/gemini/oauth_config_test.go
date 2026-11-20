package gemini

import (
	"net/url"
	"strings"
	"testing"
)

func TestMissingOAuthConfigurationFailsLocally(t *testing.T) {
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "")
	c := New()
	if _, err := c.BeginLogin(t.Context(), "cli"); err == nil || !strings.Contains(err.Error(), "GEMINI_CLI_OAUTH_CLIENT_ID") {
		t.Fatal(err)
	}
	if _, err := c.token(t.Context(), url.Values{}); err == nil {
		t.Fatal("missing OAuth configuration accepted")
	}
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GEMINI_CLI_OAUTH_CLIENT_SECRET", "fixture-secret")
	c = New()
	if _, err := c.BeginLogin(t.Context(), "cli"); err != nil {
		t.Fatal(err)
	}
}
