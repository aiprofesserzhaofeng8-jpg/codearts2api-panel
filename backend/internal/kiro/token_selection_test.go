package kiro

import (
	"encoding/json"
	"testing"
)

func TestApplyTokenRequiresApplicationAccessToken(t *testing.T) {
	for _, method := range []string{"idc", "social"} {
		t.Run(method, func(t *testing.T) {
			a := fixtureOAuth()
			a.AuthMethod = method
			for _, input := range []string{`{"idToken":"identity-only","expiresIn":3600}`, `{"accessToken":"","idToken":"identity-only","expiresIn":3600}`} {
				var o map[string]json.RawMessage
				json.Unmarshal([]byte(input), &o)
				if _, err := applyToken(a, o); err == nil {
					t.Fatal("identity token accepted as API bearer")
				}
			}
			var o map[string]json.RawMessage
			json.Unmarshal([]byte(`{"accessToken":"api-bearer","idToken":"identity-only","expiresIn":3600}`), &o)
			b, err := applyToken(a, o)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decode(b)
			if err != nil || got.AccessToken != "api-bearer" || got.RefreshToken != a.RefreshToken {
				t.Fatal("wrong token selection or lost refresh token")
			}
		})
	}
}
