package codearts

import (
	"net/http"
	"testing"
)

func TestCallerIdentityReadsOfficialFlatAccountID(t *testing.T) {
	c := NewClient()
	c.HTTP.Transport = creditRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return loginResponse(r, `{"principal_id":"user-id","principal_urn":"iam::domain:user/test-user","account_id":"stable-domain"}`), nil
	})
	id, name, domain, err := c.CallerIdentity(t.Context(), &Session{AccessKey: "ak", SecretKey: "sk"})
	if err != nil || id != "user-id" || name != "test-user" || domain != "stable-domain" {
		t.Fatalf("stable identity missing: %s %s %s %v", id, name, domain, err)
	}
}
