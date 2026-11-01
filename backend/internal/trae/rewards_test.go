package trae

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDailyClaimUsesMatchingClientSource(t *testing.T) {
	for _, tc := range []struct {
		clientID string
		source   int
	}{{ClientID, 2}, {IDEClientID, 1}} {
		t.Run(tc.clientID, func(t *testing.T) {
			claimed := false
			requests := 0
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body struct {
					Source int `json:"req_source"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Source != tc.source {
					t.Fatalf("wrong client source: %+v, %v", body, err)
				}
				if r.Header.Get("X-Device-Id") != "device" {
					t.Fatal("stored device changed")
				}
				if r.URL.Path == CheckinClaimPath {
					claimed = true
					io.WriteString(w, `{"code":0}`)
					return
				}
				fmt.Fprintf(w, `{"enable":true,"checked_in":%v,"credits":100}`, claimed)
			})
			var a Credentials
			json.Unmarshal(fixtureCredentials(), &a)
			a.ClientID = tc.clientID
			raw, _ := json.Marshal(a)
			result, err := c.ClaimDaily(context.Background(), raw)
			if err != nil || !result.Confirmed() || requests != 3 {
				t.Fatalf("claim not confirmed: %+v, %v, requests=%d", result, err, requests)
			}
		})
	}
}

func TestCheckinRejectionPreservesActualHTTPStatus(t *testing.T) {
	err := errorFrom(200, []byte(`{"code":9074,"message":"secret-token"}`), false)
	if err == nil || !strings.Contains(err.Error(), "HTTP 200") || strings.Contains(err.Error(), "HTTP 429") || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("misleading or unsafe error: %v", err)
	}
}

func TestDailyClaimRequiresAccountConfirmation(t *testing.T) {
	for _, code := range []int{0, 9095, 9074} {
		for _, confirmed := range []bool{true, false} {
			t.Run(fmt.Sprint(code, confirmed), func(t *testing.T) {
				statusCalls, claims := 0, 0
				c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Cloud-IDE-JWT trae-fixture-access" || r.Header.Get("X-Device-Id") != "device" {
						t.Error("wrong credential/device")
					}
					switch r.URL.Path {
					case CheckinStatusPath:
						statusCalls++
						fmt.Fprintf(w, `{"enable":true,"checked_in":%v,"credits":100}`, statusCalls > 1 && confirmed)
					case CheckinClaimPath:
						claims++
						fmt.Fprintf(w, `{"code":%d,"message":"trae-fixture-access"}`, code)
					default:
						t.Error("unexpected path")
					}
				})
				result, err := c.ClaimDaily(context.Background(), fixtureCredentials())
				if claims != 1 {
					t.Fatal("claim repeated")
				}
				if confirmed {
					if err != nil || !result.Confirmed() {
						t.Fatal("confirmed status lost", err)
					}
				} else {
					if result.Confirmed() {
						t.Fatal("unconfirmed claim marked successful")
					}
					if code != 0 && err == nil {
						t.Fatal("business error hidden")
					}
				}
			})
		}
	}
}
func TestDailyClaimSkipsUnavailableAndAlreadyClaimed(t *testing.T) {
	for _, body := range []string{`{"enable":false,"checked_in":false}`, `{"enable":true,"checked_in":true}`, `{}`} {
		claims := 0
		c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == CheckinClaimPath {
				claims++
			}
			io.WriteString(w, body)
		})
		result, err := c.ClaimDaily(context.Background(), fixtureCredentials())
		if claims != 0 {
			t.Fatal("unnecessary mutation")
		}
		if body == `{}` && err == nil {
			t.Fatal("missing state accepted")
		}
		if body == `{"enable":false,"checked_in":false}` && result.Confirmed() {
			t.Fatal("unavailable considered claimed")
		}
	}
}
