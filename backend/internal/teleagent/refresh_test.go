package teleagent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefreshIndependentBrowserSession(t *testing.T) {
	for _, mode := range []string{"ok", "nested", "expired", "wrong-user", "unavailable", "unsupported", "terminal", "missing", "import"} {
		t.Run(mode, func(t *testing.T) {
			a := fixture()
			a.Renewable = mode != "import"
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path != RefreshPath || r.Method != "POST" || len(body) != 0 || r.Header.Get("X-Token") != a.Token || r.Header.Get("Authorization") != "" || r.Header.Get("X-Nonce") == "" {
					t.Error("incorrect refresh request")
				}
				if mode == "unsupported" {
					w.WriteHeader(404)
					return
				}
				if mode == "unavailable" {
					fmt.Fprint(w, `{"responseCode":50005}`)
					return
				}
				if mode == "terminal" {
					fmt.Fprint(w, `{"data":{"code":40115}}`)
					return
				}
				if mode == "missing" {
					fmt.Fprint(w, `{"accessToken":"x"}`)
					return
				}
				user := a.AccountID
				if mode == "wrong-user" {
					user = "other"
				}
				exp := time.Now().Add(time.Hour).Unix()
				if mode == "expired" {
					exp = time.Now().Add(-time.Hour).Unix()
				}
				claims, _ := json.Marshal(map[string]any{"accountId": user, "exp": exp})
				response := fmt.Sprintf(`{"accessToken":%q,"expiresIn":3600}`, "tenant:e30."+base64.RawURLEncoding.EncodeToString(claims)+".signature")
				if mode == "nested" {
					response = `{"data":` + response + `}`
				}
				fmt.Fprint(w, response)
			}))
			defer srv.Close()
			c := New()
			c.Origin = srv.URL
			updated, err := c.Refresh(t.Context(), raw(a))
			if mode != "ok" && mode != "nested" {
				if err == nil || updated != nil {
					t.Fatal("invalid refresh accepted")
				}
				if mode == "import" && calls != 0 {
					t.Fatal("shared session rotated")
				}
				return
			}
			fresh, _ := decode(updated)
			_, id, err := c.Normalize(updated)
			if err != nil || !id.HasRefresh || fresh.Token == a.Token || fresh.APIKey != a.APIKey || fresh.DeviceID != a.DeviceID || fresh.ExpiresAt <= time.Now().Unix() || calls != 1 {
				t.Fatal("rotation lost", err)
			}
		})
	}
}
