package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPricingRequiresCompletePricesAndAdminAuth(t *testing.T) {
	s, key := responseTestServer(t, func(r *http.Request) (*http.Response, error) { t.Fatal("must not call upstream"); return nil, nil })
	w := responseRequest(s, key, "/admin/api/pricing", `{"model":"test","price":{"currency":"USD","input":1,"output":2,"cached":0}}`)
	if w.Code != 401 && w.Code != 403 {
		t.Fatalf("API key reached admin endpoint: %d", w.Code)
	}
	for _, body := range []string{`{"model":"test","price":{"currency":"CNY","input":1,"output":2,"cached":0}}`, `{"model":"test","price":{"currency":"USD","input":1,"output":2}}`, `{"model":"test","price":{"currency":"USD","input":-1,"output":2,"cached":0}}`} {
		r := httptest.NewRequest("POST", "/admin/api/pricing", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.handleAdminPricing(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid input: %d", w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/admin/api/pricing", strings.NewReader(`{"model":"test","price":{"currency":"USD","input":1,"output":2,"cached":0}}`))
	w = httptest.NewRecorder()
	s.handleAdminPricing(w, r)
	if w.Code != 200 {
		t.Fatalf("save failed %d %s", w.Code, w.Body.String())
	}
	if s.Store.Prices()["test"].Cached != 0 {
		t.Fatal("explicit zero lost")
	}
}
