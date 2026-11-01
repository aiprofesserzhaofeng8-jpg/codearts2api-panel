package server

import (
	"codearts2api/internal/store"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/trae"
)

// This exact fixture runs against the frozen baseline and the current source.
// All native requests use fake transports/local servers and synthetic tokens.
func TestAcceptanceRewardsContract(t *testing.T) {
	var records []map[string]any
	var mu sync.Mutex
	var calls []string
	note := func(r *http.Request) { mu.Lock(); calls = append(calls, r.Method+" "+r.URL.Path); mu.Unlock() }
	stamp := regexp.MustCompile(`\d{2}:\d{2}:\d{2}`)
	record := func(label string, s *Server, w *httptest.ResponseRecorder) {
		if w.Code == 401 {
			t.Fatalf("fixture authentication did not reach %s", label)
		}
		var response any
		if err := json.Unmarshal([]byte(stamp.ReplaceAllString(w.Body.String(), "<time>")), &response); err != nil {
			t.Fatal(label, err)
		}
		// Absolute check-in timestamps vary between the two independent runs.
		if obj, ok := response.(map[string]any); ok {
			if n, ok := obj["next_claim_at"].(float64); ok {
				obj["next_claim_at"] = n > 0
			}
		}
		state := []map[string]any{}
		for _, a := range s.Store.Accounts() {
			state = append(state, map[string]any{"id": a.ID, "claim_date": a.LastClaimDate, "claim_state": a.LastClaimState, "claim_error": a.LastClaimErr, "claim_message": a.LastClaimMsg, "waiting": a.NextClaimAt > 0, "failures": a.Failures})
		}
		mu.Lock()
		sort.Strings(calls)
		copyCalls := append([]string{}, calls...)
		calls = nil
		mu.Unlock()
		records = append(records, map[string]any{"case": label, "status": w.Code, "response": response, "state": state, "upstream_calls": copyCalls})
	}
	s := newAdminTestServer(t, "a", "b")
	mode := "success"
	s.Up.HTTP = &http.Client{Transport: probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		note(r)
		if mode == "failure" {
			return probeFakeResponse(r, 503, "fixture unavailable"), nil
		}
		if mode == "invalid" {
			return probeFakeResponse(r, 200, `{`), nil
		}
		if r.URL.Path == codearts.CreditPath {
			return probeFakeResponse(r, 200, creditBody), nil
		}
		if r.URL.Path == codearts.WelfareDeliveryPath {
			if mode == "empty" {
				return probeFakeResponse(r, 200, `{"code":0,"data":{"items":[]}}`), nil
			}
			return probeFakeResponse(r, 200, `{"code":0,"data":{"items":[{"campaignId":1,"title":"daily","claimable":true,"type":"USER_LOGIN"},{"campaignId":3,"title":"invite","claimable":true,"type":"INVITE_USER"}]}}`), nil
		}
		if mode == "unconfirmed" {
			return probeFakeResponse(r, 200, `{"code":40001,"message":"not ready"}`), nil
		}
		return probeFakeResponse(r, 200, `{"code":0,"data":{}}`), nil
	})}
	for _, tc := range []struct{ label, method, path, body string }{
		{"credits/all", "GET", "/admin/api/credits", ""},
		{"credits/cache", "GET", "/admin/api/credits", ""},
		{"credits/trim-id", "GET", "/admin/api/credits?id=%20b%20", ""},
		{"credits/fresh-any-value", "GET", "/admin/api/credits?id=a&fresh=0", ""},
		{"credits/missing", "GET", "/admin/api/credits?id=missing", ""},
		{"credits/method", "POST", "/admin/api/credits", "{}"},
		{"claim/method", "GET", "/admin/api/claim", ""},
		{"claim/malformed", "POST", "/admin/api/claim", `{"id":`},
		{"claim/missing", "POST", "/admin/api/claim", `{"id":"missing"}`},
		{"claim/success", "POST", "/admin/api/claim", `{"id":"a"}`},
		{"claim/native-repeat-unchanged", "POST", "/admin/api/claim", `{"id":"a"}`},
		{"claim/null", "POST", "/admin/api/claim", `null`},
	} {
		record(tc.label, s, adminProbeRequest(t, s, tc.method, tc.path, tc.body))
	}
	for _, m := range []string{"failure", "invalid", "empty", "unconfirmed"} {
		mode = m
		record("native/credit/"+m, s, adminProbeRequest(t, s, "GET", "/admin/api/credits?id=a&fresh=1", ""))
		record("native/credit-cache/"+m, s, adminProbeRequest(t, s, "GET", "/admin/api/credits?id=a", ""))
		record("native/claim/"+m, s, adminProbeRequest(t, s, "POST", "/admin/api/claim", `{"id":"b"}`))
	}
	mode = "empty"
	s.Store.SetAccountEnabled("a", false)
	record("claim/batch-skips-disabled", s, adminProbeRequest(t, s, "POST", "/admin/api/claim", ""))
	record("claim/native-explicit-disabled", s, adminProbeRequest(t, s, "POST", "/admin/api/claim", `{"id":"a"}`))
	s = newAdminTestServer(t)
	record("credits/no-accounts", s, adminProbeRequest(t, s, "GET", "/admin/api/credits", ""))
	record("claim/no-accounts", s, adminProbeRequest(t, s, "POST", "/admin/api/claim", ""))
	mode = "success"
	wb, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		note(r)
		if mode == "failure" {
			w.WriteHeader(503)
			io.WriteString(w, `{"code":503,"message":"unavailable"}`)
			return
		}
		if mode == "invalid" {
			io.WriteString(w, `{`)
			return
		}
		io.WriteString(w, `{"code":0,"data":{},"Response":{"Data":{"Accounts":[{"CapacityRemain":70,"CapacityUsed":30,"CapacitySize":100}]}}}`)
	})
	wb.AllowLocalAdmin = true
	wb.Store.UpdateSettings(func(st *store.Settings) { st.AdminPassword = "" })
	addWB(t, wb, "cn", time.Now().Add(time.Hour).Unix())
	for _, m := range []string{"success", "failure", "invalid", "success"} {
		mode = m
		record("wb/credit/"+m, wb, adminProbeRequest(t, wb, "GET", "/admin/api/credits?fresh=1", ""))
		record("wb/credit-cache/"+m, wb, adminProbeRequest(t, wb, "GET", "/admin/api/credits", ""))
	}
	mode = "success"
	record("wb/status-unknown", wb, adminProbeRequest(t, wb, "GET", "/admin/api/welfare?id=wb-cn", ""))
	record("wb/claim", wb, adminProbeRequest(t, wb, "POST", "/admin/api/claim", `{"id":"wb-cn"}`))
	record("wb/claim-dedup", wb, adminProbeRequest(t, wb, "POST", "/admin/api/claim", `{"id":"wb-cn"}`))
	record("wb/credit-after-claim", wb, adminProbeRequest(t, wb, "GET", "/admin/api/credits", ""))
	record("wb/status-confirmed-local", wb, adminProbeRequest(t, wb, "GET", "/admin/api/welfare?id=wb-cn", ""))
	checked := false
	mode = "success"
	tr, _, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
		note(r)
		switch r.URL.Path {
		case trae.CreditPath:
			io.WriteString(w, `{"is_credits_billing":true,"summary":{"total":100,"used":30}}`)
		case trae.CheckinStatusPath:
			fmt.Fprintf(w, `{"enable":true,"checked_in":%v,"credits":100}`, checked)
		case trae.CheckinClaimPath:
			if mode == "unconfirmed" {
				io.WriteString(w, `{"code":9074,"message":"rate limited"}`)
			} else {
				checked = true
				io.WriteString(w, `{"code":0}`)
			}
		}
	})
	tr.AllowLocalAdmin = true
	tr.Store.UpdateSettings(func(st *store.Settings) { st.AdminPassword = "" })
	addTrae(t, tr)
	record("trae/status", tr, adminProbeRequest(t, tr, "GET", "/admin/api/welfare?id=trae-acct", ""))
	record("trae/credit", tr, adminProbeRequest(t, tr, "GET", "/admin/api/credits", ""))
	mode = "unconfirmed"
	record("trae/9074", tr, adminProbeRequest(t, tr, "POST", "/admin/api/claim", `{"id":"trae-acct"}`))
	record("trae/cooldown", tr, adminProbeRequest(t, tr, "POST", "/admin/api/claim", `{"id":"trae-acct"}`))
	// Restore only the synthetic test account's cooldown, then verify confirmation.
	a := tr.Store.Account("trae-acct")
	a.NextClaimAt = 0
	tr.Store.UpsertAccount(a)
	mode = "success"
	record("trae/confirmed", tr, adminProbeRequest(t, tr, "POST", "/admin/api/claim", `{"id":"trae-acct"}`))
	record("trae/dedup", tr, adminProbeRequest(t, tr, "POST", "/admin/api/claim", `{"id":"trae-acct"}`))
	record("trae/credit-invalidated", tr, adminProbeRequest(t, tr, "GET", "/admin/api/credits", ""))
	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("REWARDS_CONTRACT %s\n", raw)
}

func BenchmarkCreditsWarmHTTP(b *testing.B) {
	st, err := store.Open(filepath.Join(b.TempDir(), "state.json"))
	if err != nil {
		b.Fatal(err)
	}
	st.UpsertAccount(&codearts.Account{ID: "a", Enabled: true, Session: &codearts.Session{AccessKey: "fixture", SecretKey: "fixture", CredentialKind: "aksk"}})
	s := newConfiguredServer(st)
	s.Up.HTTP = &http.Client{Transport: probeRoundTripFunc(func(r *http.Request) (*http.Response, error) { return probeFakeResponse(r, 200, creditBody), nil })}
	s.handleAdminCredits(httptest.NewRecorder(), httptest.NewRequest("GET", "/admin/api/credits?id=a", nil))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		s.handleAdminCredits(w, httptest.NewRequest("GET", "/admin/api/credits?id=a", nil))
		if w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}
