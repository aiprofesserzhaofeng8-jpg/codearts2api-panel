package grok

import (
	"codearts2api/internal/accounts"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBillingWindowsUnitsAndUnknowns(t *testing.T) {
	calls := 0
	weekly := `{"config":{"creditUsagePercent":0,"currentPeriod":{"type":"weekly","end":"2026-11-20T00:00:00Z"},"billingPeriodEnd":"2026-12-01T00:00:00Z"}}`
	monthly := `{"config":{"monthlyLimit":{"val":15000},"used":{"val":1250},"billingPeriodEnd":"2026-12-01T00:00:00Z"}}`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/billing" || r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
			t.Error("wrong billing route")
		}
		if r.URL.Query().Get("format") == "credits" {
			w.Write([]byte(weekly))
		} else {
			w.Write([]byte(monthly))
		}
	}))
	defer s.Close()
	c := New()
	c.BaseURL = s.URL
	b, err := c.Credit(context.Background(), fixture(t))
	var credit accounts.CreditInfo
	json.Unmarshal(b, &credit)
	if err != nil || calls != 2 || len(credit.Windows) != 2 {
		t.Fatal(string(b), err)
	}
	w, m := credit.Windows[0], credit.Windows[1]
	if w.Utilization == nil || *w.Utilization != 0 || w.ResetsAt == m.ResetsAt || *m.Total != 150 || *m.Used != 12.5 || *m.Remaining != 137.5 {
		t.Fatal("wrong units/reset", string(b))
	}
	weekly = `{"config":{}}`
	monthly = `{"config":{}}`
	b, err = c.Credit(context.Background(), fixture(t))
	credit = accounts.CreditInfo{}
	json.Unmarshal(b, &credit)
	if err != nil || credit.Available {
		t.Fatal("unknown became zero")
	}
	for _, bad := range []string{`{"config":{"monthlyLimit":{"other":10}}}`, `{"config":{"used":-1}}`, `{"config":{"used":"NaN"}}`} {
		monthly = bad
		if _, err = c.Credit(context.Background(), fixture(t)); err == nil {
			t.Fatal("invalid billing accepted")
		}
	}
}
