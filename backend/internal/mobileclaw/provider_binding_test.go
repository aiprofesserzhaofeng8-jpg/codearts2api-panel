package mobileclaw

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQRProviderBinding(t *testing.T) {
	for _, mode := range []string{"ok", "read-retry", "ambiguous", "unavailable", "wrong-provider", "missing-provider", "missing-refresh", "wrong-user", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			jwt := func(provider string) string {
				b, _ := json.Marshal(map[string]any{"uid": "user", "aip": provider, "exp": time.Now().Add(time.Hour).Unix()})
				return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".fixture"
			}
			rotations, reads := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/desktop/manager/user-subjects":
					reads++
					if mode == "read-retry" && reads == 2 {
						w.WriteHeader(503)
						return
					}
					fmt.Fprint(w, `[{"userId":"user"}]`)
				case "/desktop/manager/subscriptions/getAiProviders":
					if mode == "unavailable" {
						fmt.Fprint(w, `[]`)
						return
					}
					if mode == "ambiguous" {
						fmt.Fprint(w, `[{"provider":"one","packageList":[{"providerType":"FREE","available":true}]},{"provider":"two","packageList":[{"providerType":"FREE","available":true}]}]`)
						return
					}
					fmt.Fprint(w, `[{"provider":"CIDC-RP-48","packageList":[{"providerType":"FREE","available":true}]}]`)
				case "/desktop/manager/auth/refresh":
					rotations++
					var d loginDevice
					_ = json.NewDecoder(r.Body).Decode(&d)
					if d.AIProvider != "CIDC-RP-48" || d.UserID != "user" || d.DeviceID != "panel" || d.QRID != "" || r.Header.Get("Authorization") != "Bearer initial" {
						t.Error("incorrect binding request")
					}
					if mode == "timeout" {
						w.WriteHeader(504)
						return
					}
					provider := "CIDC-RP-48"
					if mode == "wrong-provider" {
						provider = "other"
					}
					if mode == "missing-provider" {
						provider = ""
					}
					token := jwt(provider)
					if mode == "wrong-user" {
						b, _ := json.Marshal(map[string]any{"uid": "other", "aip": provider, "exp": time.Now().Add(time.Hour).Unix()})
						token = "e30." + base64.RawURLEncoding.EncodeToString(b) + ".fixture"
					}
					refresh := "rotated"
					if mode == "missing-refresh" {
						refresh = ""
					}
					fmt.Fprintf(w, `{"accessToken":%q,"refreshToken":%q}`, token, refresh)
				default:
					t.Error("unexpected path", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			c := New()
			c.Origin = srv.URL
			exchange, _ := json.Marshal(map[string]string{"accessToken": jwt(""), "refreshToken": "initial"})
			c.logins = map[string]*loginPending{"test": {device: loginDevice{DeviceID: "panel", MAC: "00:11:22:33:44:55"}, exchange: exchange, expires: time.Now().Add(time.Minute)}}
			for i := 0; i < 2; i++ {
				raw, ready, err := c.CheckLogin(t.Context(), "cn", "test")
				success := mode == "ok" || mode == "read-retry" && i == 1
				if success {
					if err != nil || !ready {
						t.Fatal("login failed", err)
					}
					var a Credentials
					_ = json.Unmarshal(raw, &a)
					if a.AIProvider != "CIDC-RP-48" || a.RefreshToken != "rotated" {
						t.Fatal("binding or rotated token lost")
					}
				} else if err == nil || ready || raw != nil {
					t.Fatal("invalid binding accepted")
				}
			}
			want := 1
			if mode == "ambiguous" || mode == "unavailable" {
				want = 0
			}
			if rotations != want {
				t.Fatal("rotating request replayed", rotations)
			}
		})
	}
}
