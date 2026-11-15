package accounts_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/mobileclaw"
	"codearts2api/internal/storage"
	"codearts2api/internal/teleagent"
)

// Exercise the real adapters through the shared lock/CAS/storage path. A
// rotated token must survive a failed disk write in memory, never rotate twice
// for concurrent callers, and become durable once storage is repaired.
func TestPanelRenewalAdaptersSerializeAndRetainRotatedCredentials(t *testing.T) {
	for _, platform := range []string{"mobileclaw", "teleagent"} {
		for _, failDisk := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/diskFailure=%v", platform, failDisk), func(t *testing.T) {
				jwt := func(exp int64) string {
					claims, _ := json.Marshal(map[string]any{"uid": "user", "accountId": "user", "exp": exp, "aip": "CIDC-RP-48"})
					return "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture"
				}
				old, fresh := jwt(time.Now().Add(-time.Minute).Unix()), jwt(time.Now().Add(time.Hour).Unix())
				var calls atomic.Int32
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					fmt.Fprintf(w, `{"accessToken":%q,"refreshToken":"rotated-refresh","expiresIn":3600}`, fresh)
				}))
				defer up.Close()
				var adapter accounts.CredentialAdapter
				var credential any
				if platform == "mobileclaw" {
					c := mobileclaw.New()
					c.Origin = up.URL
					adapter = c
					credential = map[string]any{"token": old, "aiProvider": "CIDC-RP-48", "userId": "user", "deviceId": "panel-device", "refreshToken": "old-refresh", "refreshDevice": map[string]string{"deviceId": "panel-device", "mac": "00:11:22:33:44:55", "userId": "user"}}
				} else {
					c := teleagent.New()
					c.Origin = up.URL
					adapter = c
					credential = teleagent.Credentials{Token: old, APIKey: "sk-fixture", AccountID: "user", DeviceID: "panel-device", InstallID: "fixture-install", AppVersion: "3.0.0", Renewable: true}
				}
				raw, _ := json.Marshal(credential)
				dir := filepath.Join(t.TempDir(), "data")
				st, err := storage.Open(filepath.Join(dir, "state.json"))
				if err != nil {
					t.Fatal(err)
				}
				st.UpsertAccount(&accounts.Account{ID: "account", Provider: platform, Scope: "cn", ExternalID: "user", Credentials: raw})
				if err = st.Save(); err != nil {
					t.Fatal(err)
				}
				if failDisk {
					if err = os.Rename(dir, dir+"-backup"); err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(dir, []byte("blocked"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				runtime := accounts.NewProviderCredentials(st, func() bool { return true })
				var wg sync.WaitGroup
				var failures atomic.Int32
				for range 5 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if _, err := runtime.Resolve(t.Context(), "account", adapter); err != nil {
							failures.Add(1)
						}
					}()
				}
				wg.Wait()
				wantFailures := int32(0)
				if failDisk {
					wantFailures = 1
				}
				if calls.Load() != 1 || failures.Load() != wantFailures {
					t.Fatal("renewal repeated or failure hidden", calls.Load(), failures.Load())
				}
				_, id, err := adapter.Normalize(st.Account("account").Credentials)
				if err != nil || !id.HasRefresh || id.ExpiresAt <= time.Now().Unix() {
					t.Fatal("rotated generation lost", err)
				}
				if failDisk {
					if err = os.Remove(dir); err != nil {
						t.Fatal(err)
					}
					if err = os.Rename(dir+"-backup", dir); err != nil {
						t.Fatal(err)
					}
					if err = st.Save(); err != nil {
						t.Fatal(err)
					}
				}
				reopened, err := storage.Open(st.Path())
				if err != nil {
					t.Fatal(err)
				}
				_, id, err = adapter.Normalize(reopened.Account("account").Credentials)
				if err != nil || id.ExpiresAt <= time.Now().Unix() {
					t.Fatal("new credentials not durable", err)
				}
				if platform == "mobileclaw" {
					var saved mobileclaw.Credentials
					if err := json.Unmarshal(reopened.Account("account").Credentials, &saved); err != nil || saved.AIProvider != "CIDC-RP-48" || saved.RefreshToken != "rotated-refresh" {
						t.Fatal("supplier binding or rotated refresh token not durable")
					}
				}
			})
		}
	}
}
