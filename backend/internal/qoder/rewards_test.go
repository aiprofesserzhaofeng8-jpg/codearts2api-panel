package qoder

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCampaignClaimConfirmation(t *testing.T) {
	for _, scope := range []string{"cn", "global"} {
		t.Run(scope, func(t *testing.T) {
			claims, reads := 0, 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cosy-ClientType") != "10" || r.Header.Get("Authorization") != "Bearer dt-private-fixture" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("wrong billing headers")
				}
				if r.Method == "POST" {
					claims++
					b, _ := io.ReadAll(r.Body)
					if r.URL.Path != CampaignsPath+"/daily-fixture/claim" || string(b) != "{}" {
						t.Error("wrong mutation")
					}
					io.WriteString(w, `{"status":"CLAIMED"}`)
					return
				}
				reads++
				status := "CLAIMABLE"
				if claims > 0 {
					status = "CLAIMED"
				}
				fmt.Fprintf(w, `{"campaigns":[{"campaignId":"daily-fixture","actionType":"CLAIM_BENEFIT","claimStatus":%q,"benefit":{"kind":"CREDITS","amount":100}}]}`, status)
			})
			var a Credentials
			json.Unmarshal(credential(), &a)
			a.Scope = scope
			raw, _ := json.Marshal(a)
			r, e := c.DailyStatus(t.Context(), raw)
			if e != nil || r.State != "claimable" || claims != 0 {
				t.Fatal(r, e)
			}
			r, e = c.ClaimDaily(t.Context(), raw)
			if e != nil || r.State != "claimed" || claims != 1 || reads != 3 {
				t.Fatal(r, e, claims, reads)
			}
			r, e = c.ClaimDaily(t.Context(), raw)
			if e != nil || r.State != "already_claimed" || claims != 1 {
				t.Fatal(r, e, claims)
			}
		})
	}
}

func TestCampaignNeverClaimsIneligible(t *testing.T) {
	for _, row := range []string{
		``,
		`{"campaignId":"../unsafe","claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100}}`,
		`{"campaignId":"pro","claimStatus":"CLAIMABLE","benefit":{"kind":"SUBSCRIPTION","amount":1}}`,
		`{"campaignId":"daily","actionType":"VIEW_DETAILS","claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100}}`,
		`{"campaignId":"daily","requiredAchievementKey":"invite","claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100}}`,
		`{"campaignId":"daily","endAt":1,"claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100}}`,
	} {
		t.Run(row, func(t *testing.T) {
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("unexpected claim")
				}
				fmt.Fprintf(w, `{"campaigns":[%s]}`, row)
			})
			r, e := c.ClaimDaily(t.Context(), credential())
			if e != nil || r.Confirmed() {
				t.Fatal(r, e)
			}
		})
	}
}

func TestCampaignNoFalseReceiptOrReplay(t *testing.T) {
	for _, status := range []int{200, 409, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			claims := 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					claims++
					w.WriteHeader(status)
					io.WriteString(w, `{"status":"CLAIMED"}`)
					return
				}
				io.WriteString(w, `{"campaigns":[{"campaignId":"daily","claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100}},{"campaignId":"other","claimStatus":"CLAIMED","benefit":{"kind":"CREDITS","amount":10}}]}`)
			})
			r, e := c.ClaimDaily(t.Context(), credential())
			if r.Confirmed() || claims != 1 {
				t.Fatal(r, e, claims)
			}
		})
	}
}

func TestCampaignMalformedAndBusinessErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"campaigns":null}`, `{"success":false,"campaigns":[]}`, `{"code":401,"data":{"campaigns":[]}}`} {
		c := fixture(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		if _, e := c.DailyStatus(t.Context(), credential()); e == nil {
			t.Fatal("accepted", body)
		}
	}
}

func TestCampaignRedirectNotFollowed(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "leak") {
			t.Error("redirect followed")
		}
		http.Redirect(w, r, "/leak", 307)
	})
	if _, e := c.DailyStatus(t.Context(), credential()); e == nil {
		t.Fatal("redirect accepted")
	}
}

func TestCampaignMutationFailureReconcilesWithoutReplay(t *testing.T) {
	claims := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			claims++
			w.WriteHeader(502)
			io.WriteString(w, `{}`)
			return
		}
		state := "CLAIMABLE"
		if claims > 0 {
			state = "CLAIMED"
		}
		row := fmt.Sprintf(`{"campaignId":"daily","claimStatus":%q,"benefit":{"kind":"CREDITS","amount":100}}`, state)
		fmt.Fprintf(w, `{"campaigns":[%s,%s]}`, row, row)
	})
	result, err := c.ClaimDaily(t.Context(), credential())
	if err != nil || !result.Confirmed() || claims != 1 || result.Credits == nil || *result.Credits != 100 {
		t.Fatal(result, err, claims)
	}
}
