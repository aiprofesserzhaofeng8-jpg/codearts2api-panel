package workbuddy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDailyCheckinResultAndNoReplay(t *testing.T) {
	for _, tc := range []struct {
		body      string
		status    int
		confirmed bool
	}{{`{"code":0,"data":{}}`, 200, true}, {`{"code":14001,"msg":"今日已签到"}`, 200, true}, {`{"code":14001,"msg":"already in another activity"}`, 200, false}, {`{"code":12153,"msg":"token expired"}`, 200, false}, {`{}`, 200, false}, {`{"code":0}`, 500, false}} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/v2/billing/meter/daily-checkin" || r.Header.Get("X-Refresh-Token") != "" {
				t.Error("wrong claim request")
			}
			w.WriteHeader(tc.status)
			io.WriteString(w, tc.body)
		}))
		c := New()
		c.CNBilling = srv.URL
		r, err := c.ClaimDaily(context.Background(), fixtureAuth("cn"))
		srv.Close()
		if calls != 1 || r.Confirmed() != tc.confirmed || (tc.confirmed && err != nil) || (!tc.confirmed && err == nil) {
			t.Fatalf("bad result: calls=%d result=%+v err=%v", calls, r, err)
		}
	}
}
func TestGlobalCheckinOnlyFallsBackOn404(t *testing.T) {
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Path == "/billing/meter/daily-checkin" {
			w.WriteHeader(404)
			return
		}
		io.WriteString(w, `{"code":0,"data":{}}`)
	}))
	defer srv.Close()
	c := New()
	c.GlobalBase = srv.URL
	r, err := c.ClaimDaily(context.Background(), fixtureAuth("global"))
	if err != nil || !r.Confirmed() || len(calls) != 2 {
		t.Fatal("global routing fallback failed", err)
	}
}
