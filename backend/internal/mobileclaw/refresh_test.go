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

func TestRefreshIndependentSession(t *testing.T) {
	for _, mode := range []string{"ok", "unauthorized", "unavailable", "missing", "wrong-user", "wrong-provider", "missing-provider", "import"} {
		t.Run(mode, func(t *testing.T) {
			var a Credentials
			_ = json.Unmarshal(fixture(t), &a)
			a.RefreshToken = "refresh-old"
			a.RefreshDevice = &loginDevice{DeviceID: a.DeviceID, MAC: "00:11:22:33:44:55", UserID: a.UserID}
			if mode == "import" {
				a.RefreshToken = ""
				a.RefreshDevice = nil
			}
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var d loginDevice
				_ = json.NewDecoder(r.Body).Decode(&d)
				if r.URL.Path != "/desktop/manager/auth/refresh" || r.Header.Get("Authorization") != "Bearer refresh-old" || d.AIProvider != "CIDC-RP-48" || d.UserID != "user" || d.DeviceID != "device" || d.QRID != "" {
					t.Error("incorrect refresh request")
				}
				if mode == "unauthorized" {
					w.WriteHeader(401)
					return
				}
				if mode == "unavailable" {
					w.WriteHeader(503)
					return
				}
				if mode == "missing" {
					fmt.Fprint(w, `{}`)
					return
				}
				user := "user"
				if mode == "wrong-user" {
					user = "other"
				}
				provider := "CIDC-RP-48"
				if mode == "wrong-provider" {
					provider = "other"
				}
				if mode == "missing-provider" {
					provider = ""
				}
				claims, _ := json.Marshal(map[string]any{"uid": user, "aip": provider, "exp": time.Now().Add(2 * time.Hour).Unix()})
				fmt.Fprintf(w, `{"accessToken":%q,"refreshToken":"refresh-new"}`, "e30."+base64.RawURLEncoding.EncodeToString(claims)+".signature")
			}))
			defer srv.Close()
			c := New()
			c.Origin = srv.URL
			raw, _ := json.Marshal(a)
			updated, err := c.Refresh(t.Context(), raw)
			if mode != "ok" {
				if err == nil || updated != nil {
					t.Fatal("invalid refresh accepted")
				}
				if mode == "import" && calls != 0 {
					t.Fatal("shared session rotated")
				}
				return
			}
			var fresh Credentials
			_ = json.Unmarshal(updated, &fresh)
			_, id, err := c.Normalize(updated)
			if err != nil || !id.HasRefresh || fresh.RefreshToken != "refresh-new" || fresh.Token == a.Token || calls != 1 {
				t.Fatal("rotation lost", err)
			}
		})
	}
}
