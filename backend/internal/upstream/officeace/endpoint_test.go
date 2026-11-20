package officeace

import "testing"

func TestCredentialEndpointPinned(t *testing.T) {
	c := New()
	for _, bad := range []string{"http://api.modelarts-maas.com/v2", "https://other.invalid/v2", DefaultMaaSBase + "/other", DefaultMaaSBase + ":8443/v2"} {
		if _, err := c.maasBase(Credentials{MaaSBaseURL: bad}); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, good := range []string{"", DefaultMaaSBase, "api.modelarts-maas.com/v2", DefaultMaaSBase + "/v2/"} {
		if target, err := c.maasBase(Credentials{MaaSBaseURL: good}); err != nil || target != DefaultMaaSBase+"/v2" {
			t.Fatalf("unexpected target for %q", good)
		}
	}
}
