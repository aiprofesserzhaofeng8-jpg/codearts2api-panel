package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/storage"
	"codearts2api/internal/trae"
)

// The identical fixture also runs against the frozen pre-migration checkout.
// Native transport, credentials and invitations here are entirely synthetic.
func TestAcceptanceWelfareContract(t *testing.T) {
	records := []map[string]any{}
	calls := []string{}
	mode := "normal"
	native := func(s *Server) {
		s.Up.HTTP = &http.Client{Transport: probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			call := r.Method + " " + r.URL.RequestURI()
			if r.Body != nil {
				raw, _ := io.ReadAll(r.Body)
				call += " " + string(raw)
			}
			calls = append(calls, call)
			deadline, ok := r.Context().Deadline()
			if !ok || time.Until(deadline) > 30*time.Second {
				t.Fatal("request deadline lost")
			}
			if err := r.Context().Err(); err != nil {
				return nil, err
			}
			switch mode {
			case "failure":
				return probeFakeResponse(r, 503, "fixture unavailable"), nil
			case "business":
				return probeFakeResponse(r, 200, `{"code":40001,"message":"fixture rejected"}`), nil
			case "invalid":
				return probeFakeResponse(r, 200, `{`), nil
			}
			if r.URL.Path == codearts.WelfareDeliveryPath {
				if mode == "empty" {
					return probeFakeResponse(r, 200, `{"code":0,"data":{"items":[]}}`), nil
				}
				return probeFakeResponse(r, 200, `{"code":0,"data":{"items":[{"campaignId":1,"title":"签到 <daily>","claimable":true,"type":"USER_LOGIN","status":"ACTIVE","benefitAmount":1000.5,"benefitUnit":"CREDIT","description":"daily","extra":{"startTime":"start","endTime":"end","triggerMode":"MANUAL_CLAIM","triggerEvent":"user.login"}},{"campaignId":3,"title":"邀请","claimable":true,"type":"INVITE_USER"},{"campaignId":4,"title":"学生","claimable":true,"type":"STUDENT_CERTIFIED"},{"campaignId":5,"claimable":true,"extra":{"triggerMode":"EVENT"}},{"campaignId":6,"claimable":false}]}}`), nil
			}
			if r.URL.Path != codearts.WelfareInvitePath {
				t.Fatal("unexpected native call", r.URL.Path)
			}
			return probeFakeResponse(r, 200, `{"code":0,"data":{"campaignId":7,"inviteCode":"fixture-invite","inviteUrl":"https://example.invalid/join?a=1&b=2","valid":true,"status":"ACTIVE","activationCount":2,"maxActivations":10}}`), nil
		})}
	}
	record := func(label string, s *Server, w *httptest.ResponseRecorder) {
		if w.Code == 401 {
			t.Fatal("fixture authentication failed", label)
		}
		var response any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(label, err)
		}
		state := []map[string]any{}
		for _, a := range s.Store.Accounts() {
			state = append(state, map[string]any{"id": a.ID, "enabled": a.Enabled, "claim_date": a.LastClaimDate, "claim_state": a.LastClaimState, "waiting": a.NextClaimAt > 0, "failures": a.Failures})
		}
		records = append(records, map[string]any{"case": label, "status": w.Code, "response": response, "wire": w.Body.String(), "state": state, "upstream_calls": append([]string{}, calls...)})
		calls = nil
	}
	request := func(label string, s *Server, method, path, body string) {
		record(label, s, adminProbeRequest(t, s, method, path, body))
	}
	s := newAdminTestServer(t, "a", "b")
	native(s)
	for _, tc := range []struct{ label, method, path, body string }{
		{"list/explicit", "GET", "/admin/api/welfare?id=b", ""},
		{"list/implicit", "GET", "/admin/api/welfare", ""},
		{"list/missing-falls-back", "GET", "/admin/api/welfare?id=missing", ""},
		{"list/untrimmed-id", "GET", "/admin/api/welfare?id=%20b%20", ""},
		{"list/post-compatible", "POST", "/admin/api/welfare?id=a", "{"},
		{"invite/method", "GET", "/admin/api/invite", ""},
		{"invite/default", "POST", "/admin/api/invite", `{"id":"a"}`},
		{"invite/custom", "POST", "/admin/api/invite", `{"id":"b","campaign_id":7}`},
		{"invite/negative-compatible", "POST", "/admin/api/invite", `{"id":"a","campaign_id":-2}`},
		{"invite/malformed-compatible", "POST", "/admin/api/invite", `{"id":`},
		{"invite/null-compatible", "POST", "/admin/api/invite", `null`},
		{"invite/trailing-compatible", "POST", "/admin/api/invite", `{"id":"a"} {"extra":1}`},
		{"invite/unknown-compatible", "POST", "/admin/api/invite", `{"id":"a","extra":1}`},
		{"invite/partial-type-compatible", "POST", "/admin/api/invite", `{"id":"b","campaign_id":"bad"}`},
	} {
		request(tc.label, s, tc.method, tc.path, tc.body)
	}
	for _, m := range []string{"empty", "failure", "business", "invalid"} {
		mode = m
		request("list/"+m, s, "GET", "/admin/api/welfare?id=a", "")
		request("invite/"+m, s, "POST", "/admin/api/invite", `{"id":"a"}`)
	}
	mode = "normal"
	s.Store.SetAccountEnabled("a", false)
	request("list/disabled-explicit", s, "GET", "/admin/api/welfare?id=a", "")
	request("invite/disabled-explicit", s, "POST", "/admin/api/invite", `{"id":"a"}`)
	s = newAdminTestServer(t)
	native(s)
	request("list/no-account", s, "GET", "/admin/api/welfare", "")
	request("invite/no-account", s, "POST", "/admin/api/invite", "")
	s.Store.UpsertAccount(&codearts.Account{ID: "broken", Enabled: true})
	request("list/credential-error", s, "GET", "/admin/api/welfare?id=broken", "")
	request("invite/credential-error", s, "POST", "/admin/api/invite", `{"id":"broken"}`)
	wb, _, _ := nativeWBServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("status must not submit WorkBuddy claims")
		w.WriteHeader(500)
	})
	wb.Store.UpdateSettings(func(st *storage.Settings) { st.AdminPassword = "" })
	addWB(t, wb, "cn", time.Now().Add(time.Hour).Unix())
	native(wb)
	wb.Store.SetAccountEnabled("wb-cn", false)
	request("wb/disabled-status", wb, "GET", "/admin/api/welfare?id=wb-cn", "")
	request("wb/invite-keeps-native-error", wb, "POST", "/admin/api/invite", `{"id":"wb-cn"}`)
	wb.Store.SetAccountEnabled("wb-cn", true)
	a := wb.Store.Account("wb-cn")
	a.Paused = true
	wb.Store.UpsertAccount(a)
	request("wb/paused-fallback", wb, "GET", "/admin/api/welfare", "")
	request("wb/invite-no-daily-fallback", wb, "POST", "/admin/api/invite", "")
	checked, enabled, credits := false, true, true
	tr, _, _ := nativeTraeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != trae.CheckinStatusPath {
			t.Error("read-only status submitted unexpected request", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(raw))
		switch mode {
		case "failure":
			w.WriteHeader(503)
			io.WriteString(w, `{"code":503}`)
			return
		case "invalid":
			io.WriteString(w, `{}`)
			return
		}
		if credits {
			fmt.Fprintf(w, `{"enable":%v,"checked_in":%v,"credits":100}`, enabled, checked)
		} else {
			fmt.Fprintf(w, `{"enable":%v,"checked_in":%v}`, enabled, checked)
		}
	})
	tr.Store.UpdateSettings(func(st *storage.Settings) { st.AdminPassword = "" })
	addTrae(t, tr)
	request("trae/claimable", tr, "GET", "/admin/api/welfare?id=trae-acct", "")
	credits = false
	request("trae/no-amount-field", tr, "GET", "/admin/api/welfare?id=trae-acct", "")
	enabled = false
	request("trae/unavailable", tr, "GET", "/admin/api/welfare?id=trae-acct", "")
	for _, m := range []string{"failure", "invalid"} {
		mode = m
		request("trae/"+m, tr, "GET", "/admin/api/welfare?id=trae-acct", "")
	}
	mode = "normal"
	enabled = true
	checked = true
	a = tr.Store.Account("trae-acct")
	a.NextClaimAt = 12345
	tr.Store.UpsertAccount(a)
	request("trae/confirmed-preserves-response-snapshot", tr, "GET", "/admin/api/welfare?id=trae-acct", "")
	request("trae/local-confirmed-no-rpc", tr, "GET", "/admin/api/welfare?id=trae-acct", "")
	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("WELFARE_CONTRACT %s\n", raw)
}

func TestWelfareRequestCancellation(t *testing.T) {
	for _, path := range []string{"/admin/api/welfare?id=a", "/admin/api/invite"} {
		t.Run(path, func(t *testing.T) {
			s := newAdminTestServer(t, "a")
			calls := 0
			s.Up.HTTP = &http.Client{Transport: probeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Context().Err() != context.Canceled {
					t.Error("cancellation lost")
				}
				return nil, r.Context().Err()
			})}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			req := httptest.NewRequest("POST", path, strings.NewReader(`{"id":"a"}`)).WithContext(ctx)
			w := httptest.NewRecorder()
			if strings.Contains(path, "welfare") {
				s.handleAdminWelfare(w, req)
			} else {
				s.handleAdminInvite(w, req)
			}
			if w.Code != 502 || calls > 1 {
				t.Fatalf("status=%d calls=%d", w.Code, calls)
			}
		})
	}
}
