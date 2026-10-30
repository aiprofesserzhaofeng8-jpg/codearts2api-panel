package workbuddy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGrowthBuddyStatusRequiresExplicitOwnershipAndAgreement(t *testing.T) {
	for _, tc := range []struct {
		info, agreement     string
		owned, agreed, fail bool
	}{
		{`{"buddy":null}`, `{"agreed":false}`, false, false, false},
		{`{"buddy":null}`, `{"agreed":true}`, false, true, false},
		{`{"buddy":{"id":123}}`, `{}`, true, false, false},
		{`{"buddy":{"id":"123"}}`, `{}`, true, false, false},
		{`{}`, `{"agreed":true}`, false, false, true},
		{`{"buddy":{}}`, `{"agreed":true}`, false, false, true},
		{`{"buddy":{"id":0}}`, `{"agreed":true}`, false, false, true},
		{`{"buddy":null}`, `{}`, false, false, true},
		{`{"buddy":null}`, `{"agreed":"true"}`, false, false, true},
	} {
		t.Run(tc.info+tc.agreement, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("status query wrote upstream")
				}
				data := tc.info
				if r.URL.Path == GrowthBuddyAgreementPath {
					data = tc.agreement
				}
				fmt.Fprintf(w, `{"code":0,"data":%s}`, data)
			}))
			defer up.Close()
			c := New()
			c.CNBase = up.URL
			s, err := c.GrowthBuddyStatus(context.Background(), fixtureAuth("cn"))
			if (err != nil) != tc.fail || !tc.fail && (s.Owned != tc.owned || s.Agreed != tc.agreed) {
				t.Fatal(s, err)
			}
		})
	}
}

func TestGrowthBuddyCommandsAndActivityUseCorrectDomains(t *testing.T) {
	var chat, report, agree, adopt int
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/report" {
			t.Error("unexpected billing request", r.URL.Path)
		}
		report++
		io.WriteString(w, `{"code":0}`)
	}))
	defer billing.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Client-Platform") != "" {
			t.Error("web/mp headers leaked")
		}
		switch r.URL.Path {
		case GrowthBuddyAgreementPath:
			agree++
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"agree":true}` {
				t.Error("missing explicit agreement")
			}
			io.WriteString(w, `{"code":0,"data":null}`)
		case GrowthBuddyFirstPath:
			adopt++
			io.WriteString(w, `{"code":0}`)
		case "/v2/chat/completions":
			chat++
			io.WriteString(w, "data: {\"id\":\"real-onboarding\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Error("unexpected chat-domain request", r.URL.Path)
		}
	}))
	defer up.Close()
	c := New()
	c.CNBase = up.URL
	c.CNBilling = billing.URL
	c.CNWeb = "http://invalid.invalid"
	if err := c.ReportGrowthActivity(context.Background(), fixtureAuth("cn"), "first_buddy"); err != nil {
		t.Fatal(err)
	}
	if err := c.AgreeGrowthBuddy(context.Background(), fixtureAuth("cn")); err != nil {
		t.Fatal(err)
	}
	if err := c.AdoptGrowthBuddy(context.Background(), fixtureAuth("cn")); err != nil {
		t.Fatal(err)
	}
	if chat != 0 || report != 1 || agree != 1 || adopt != 1 {
		t.Fatal(chat, report, agree, adopt)
	}
}
