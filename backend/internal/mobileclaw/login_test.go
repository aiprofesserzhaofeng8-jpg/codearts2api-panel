package mobileclaw

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestQRLoginPendingPreviewAndRelease(t *testing.T) {
	token := Credentials{}
	_ = json.Unmarshal(fixture(t), &token)
	state := "0"
	exchanges := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/desktop/manager/auth/qrcode":
			var d loginDevice
			_ = json.NewDecoder(r.Body).Decode(&d)
			if d.DeviceID == "" || d.MAC == "" {
				t.Error("missing device")
			}
			fmt.Fprintf(w, `{"qrId":"qr-fixture","qrCodeUrl":"https://www.cmpassport.com/h5/js/jssdk_yw_auth/auth/scanAuth.html?traceId=fixture","qrCodeBase64":%q,"expiresInSeconds":120}`, " \n"+base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nfixture"))+" ")
		case "/desktop/manager/auth/qrstate":
			fmt.Fprintf(w, `{"result":%q}`, state)
		case "/desktop/manager/auth/qrlogin":
			exchanges++
			fmt.Fprintf(w, `{"accessToken":%q}`, token.Token)
		case "/desktop/manager/user-subjects":
			fmt.Fprint(w, `[{"userId":"user","username":"fixture"}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := New()
	c.Origin = srv.URL
	login, err := c.BeginLogin(t.Context(), "cn")
	if err != nil || !strings.HasPrefix(login.QRCode, "data:image/png;base64,") || login.ExpiresAt > time.Now().Add(121*time.Second).Unix() {
		t.Fatal(login, err)
	}
	if raw, ready, err := c.CheckLogin(t.Context(), "cn", login.State); err != nil || ready || raw != nil {
		t.Fatal("pending consumed", err)
	}
	f := c.logins[login.State]
	f.mu.Lock()
	f.nextPoll = time.Time{}
	f.mu.Unlock()
	state = "2"
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, ready, err := c.CheckLogin(t.Context(), "cn", login.State)
			if err != nil || !ready || len(raw) == 0 {
				t.Error("preview failed", err)
			}
		}()
	}
	wg.Wait()
	if exchanges != 1 {
		t.Fatal("authorization exchanged more than once", exchanges)
	}
	c.ReleaseLogin(login.State)
	if _, _, err := c.CheckLogin(t.Context(), "cn", login.State); err == nil {
		t.Fatal("released login revived")
	}
}
func TestQRLoginValidationAndExpiry(t *testing.T) {
	for _, u := range []string{"http://www.cmpassport.com/h5/js/jssdk_yw_auth/auth/scanAuth.html?x=1", "https://www.cmpassport.com.evil.test/h5/js/jssdk_yw_auth/auth/scanAuth.html?x=1", "https://user@www.cmpassport.com/h5/js/jssdk_yw_auth/auth/scanAuth.html?x=1"} {
		if trustedLoginURL(u) {
			t.Fatal("untrusted URL accepted")
		}
	}
	c := New()
	c.logins = map[string]*loginPending{"expired": {expires: time.Now().Add(time.Minute), deadline: time.Now().Add(-time.Second)}}
	if _, _, err := c.CheckLogin(t.Context(), "cn", "expired"); err == nil {
		t.Fatal("expired login accepted")
	}
	if _, err := c.BeginLogin(t.Context(), "global"); err == nil {
		t.Fatal("unsupported scope accepted")
	}
}

func TestQRLoginBindsOnlyOneAuthorizedSubject(t *testing.T) {
	var account Credentials
	_ = json.Unmarshal(fixture(t), &account)
	first := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix()))) + ".fixture"
	for _, many := range []bool{false, true} {
		t.Run(fmt.Sprint(many), func(t *testing.T) {
			refreshes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/desktop/manager/user-subjects":
					if many {
						fmt.Fprint(w, `[{"userId":"user"},{"userId":"other"}]`)
					} else {
						fmt.Fprint(w, `[{"userId":"user","username":"fixture"}]`)
					}
				case "/desktop/manager/auth/refresh":
					refreshes++
					var d loginDevice
					_ = json.NewDecoder(r.Body).Decode(&d)
					if d.UserID != "user" || d.QRID != "" || r.Header.Get("Authorization") != "Bearer refresh-fixture" {
						t.Error("incorrect subject binding")
					}
					fmt.Fprintf(w, `{"accessToken":%q,"refreshToken":"refresh-rotated"}`, account.Token)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			c := New()
			c.Origin = srv.URL
			exchange, _ := json.Marshal(map[string]string{"accessToken": first, "refreshToken": "refresh-fixture"})
			c.logins = map[string]*loginPending{"fixture": {device: loginDevice{DeviceID: "device", QRID: "qr", MAC: "00:11:22:33:44:55"}, exchange: exchange, expires: time.Now().Add(time.Minute)}}
			raw, ready, err := c.CheckLogin(t.Context(), "cn", "fixture")
			if many {
				if err == nil || ready || refreshes != 0 {
					t.Fatal("ambiguous subject silently bound")
				}
			} else {
				if err != nil || !ready || refreshes != 1 || strings.Contains(string(raw), "refresh-fixture") || !strings.Contains(string(raw), "refresh-rotated") {
					t.Fatal("binding failed or refresh material leaked", err)
				}
			}
		})
	}
}
