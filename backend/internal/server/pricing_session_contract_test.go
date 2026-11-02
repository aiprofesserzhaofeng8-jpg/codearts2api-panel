package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/storage"
)

// HTTP fixture covers pricing and the current unified session contract.
// All credentials and prices are synthetic; no upstream requests are made.
func TestAcceptancePricingSessionContract(t *testing.T) {
	records := []map[string]any{}
	s := newAdminTestServer(t)
	// Legacy plaintext verification is retained; later cases exercise the hash.
	s.Store.UpdateSettings(func(st *storage.Settings) { st.AdminPassword = "contract-secret" })
	var cookie *http.Cookie
	call := func(label, method, path, body, password, origin, site string, tlsOn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://panel.example"+path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.1:1234"
		if tlsOn {
			r.TLS = &tls.ConnectionState{}
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if password != "" {
			r.AddCookie(adminTestCookie(s, password))
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(label, err)
		}
		if expiry, ok := response["expires_at"].(float64); ok {
			remaining := expiry - float64(time.Now().Unix())
			response["expires_at"] = remaining >= 28798 && remaining <= 28800
		}
		normalizePrices := func(prices map[string]any) {
			for _, raw := range prices {
				if price, ok := raw.(map[string]any); ok && price["source"] != "DeepSeek 官方高峰参考价（固定，不自动切换时段） https://api-docs.deepseek.com/quick_start/pricing/" {
					stamp, _ := price["updated_at"].(string)
					_, err := time.Parse(time.RFC3339, stamp)
					price["updated_at"] = err == nil
				}
			}
		}
		if prices, ok := response["prices"].(map[string]any); ok {
			normalizePrices(prices)
		}
		priceRaw, _ := json.Marshal(s.Store.Prices())
		var prices map[string]any
		json.Unmarshal(priceRaw, &prices)
		normalizePrices(prices)
		audit := []map[string]any{}
		for _, e := range s.Store.AuditEvents() {
			audit = append(audit, map[string]any{"actor": e.Actor, "operation": e.Operation, "path": e.Path, "status": e.Status, "peer": e.Peer})
		}
		cookies := []map[string]any{}
		for _, c := range w.Result().Cookies() {
			cookies = append(cookies, map[string]any{"name": c.Name, "has_token": len(c.Value) >= 40, "path": c.Path, "secure": c.Secure, "http_only": c.HttpOnly, "same_site": c.SameSite, "max_age": c.MaxAge})
		}
		records = append(records, map[string]any{"case": label, "status": w.Code, "response": response, "cookies": cookies, "cache_control": w.Header().Get("Cache-Control"), "retry_after": w.Header().Get("Retry-After"), "prices": prices, "audit": audit})
		return w
	}
	for _, tc := range []struct{ label, method, path, body, origin, site string }{
		{"login/method", "GET", "/auth/login", "", "", ""},
		{"session/method", "POST", "/auth/session", "", "", ""},
		{"session/anonymous", "GET", "/auth/session", "", "", ""},
		{"login/cross-origin", "POST", "/auth/login", `{"username":"admin","password":"contract-secret"}`, "https://evil.example", ""},
		{"login/cross-site", "POST", "/auth/login", `{"username":"admin","password":"contract-secret"}`, "", "cross-site"},
		{"login/malformed", "POST", "/auth/login", `{`, "", ""},
		{"login/null", "POST", "/auth/login", `null`, "", ""},
		{"login/oversize", "POST", "/auth/login", `{"username":"admin","password":"` + strings.Repeat("a", 9000) + `"}`, "", ""},
		{"login/wrong", "POST", "/auth/login", `{"username":"admin","password":"wrong"}`, "", ""},
		{"pricing/anonymous", "GET", "/admin/api/pricing", "", "", ""},
	} {
		call(tc.label, tc.method, tc.path, tc.body, "", tc.origin, tc.site, false)
	}
	w := call("login/success", "POST", "/auth/login", `{"username":"admin","password":"contract-secret"}`, "", "", "", true)
	if w.Code != 200 {
		t.Fatal("login fixture failed")
	}
	cookie = w.Result().Cookies()[0]
	call("session/authenticated", "GET", "/auth/session", "", "", "", "", false)
	call("pricing/read", "GET", "/admin/api/pricing", "", "", "", "", false)
	call("pricing/cross-origin", "POST", "/admin/api/pricing", `{}`, "", "https://evil.example", "", false)
	for _, tc := range []struct{ label, body string }{
		{"malformed", `{`}, {"null", `null`}, {"missing-cached", `{"model":"m","price":{"currency":"USD","input":1,"output":2}}`},
		{"empty-model", `{"model":"  ","price":{"currency":"USD","input":0,"output":0,"cached":0}}`},
		{"currency-before-length", `{"model":"` + strings.Repeat("m", 201) + `","price":{"currency":"CNY","input":1,"output":2,"cached":0}}`},
		{"negative", `{"model":"m","price":{"currency":"USD","input":-1,"output":2,"cached":0}}`},
		{"too-large", `{"model":"m","price":{"currency":"USD","input":1000001,"output":2,"cached":0}}`},
		{"source-before-model-length", `{"model":"` + strings.Repeat("m", 201) + `","price":{"currency":"USD","input":0,"output":0,"cached":0,"source":"` + strings.Repeat("s", 1001) + `"}}`},
		{"model-length", `{"model":"` + strings.Repeat("m", 201) + `","price":{"currency":"USD","input":0,"output":0,"cached":0}}`},
		{"bad-number", `{"model":"m","price":{"currency":"USD","input":1e999,"output":0,"cached":0}}`},
		{"trim-zero", `{"model":" custom-model ","price":{"currency":"USD","input":0,"output":0,"cached":0}}`},
		{"unknown-trailing", `{"model":"custom-model","extra":true,"price":{"currency":"USD","input":1,"output":2,"cached":0.25,"source":"fixture"}} null`},
	} {
		call("pricing/"+tc.label, "POST", "/admin/api/pricing", tc.body, "", "", "", false)
	}
	call("pricing/method", "DELETE", "/admin/api/pricing", "", "", "", "", false)
	call("logout/cross-site", "POST", "/auth/logout", "", "", "", "cross-site", false)
	call("logout/success", "POST", "/auth/logout", "", "", "", "", true)
	call("session/revoked", "GET", "/auth/session", "", "", "", "", false)
	call("pricing/revoked", "GET", "/admin/api/pricing", "", "", "", "", false)
	cookie = nil
	w = call("login/plain-before-hash", "POST", "/auth/login", `{"username":"admin","password":"contract-secret"}`, "", "", "", false)
	cookie = w.Result().Cookies()[0]
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}
	call("session/hash-upgrade-invalidates", "GET", "/auth/session", "", "", "", "", false)
	cookie = nil
	w = call("login/hashed", "POST", "/auth/login", `{"username":"admin","password":"contract-secret"}`, "", "", "", false)
	cookie = w.Result().Cookies()[0]
	call("session/hashed", "GET", "/auth/session", "", "", "", "", false)
	s.Store.UpdateSettings(func(st *storage.Settings) { st.AdminPassword = "new-secret" })
	call("session/password-changed", "GET", "/auth/session", "", "", "", "", false)
	call("pricing/password-changed", "GET", "/admin/api/pricing", "", "", "", "", false)
	cookie = nil
	call("pricing/new-password-session", "GET", "/admin/api/pricing", "", "new-secret", "", "", false)
	// Simulate a strict price save failure without touching the state file.
	dir := s.Store.Path() + ".blocked"
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(s.Store.Path(), s.Store.Path()+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, s.Store.Path()); err != nil {
		t.Fatal(err)
	}
	call("pricing/save-failure-rollback", "POST", "/admin/api/pricing", `{"model":"custom-model","price":{"currency":"USD","input":9,"output":9,"cached":9}}`, "new-secret", "", "", false)
	// Rate limiting keeps the original peer bucket and 20-failure threshold.
	for i := 0; i < 20; i++ {
		s.loginFailures.failure("198.51.100.1:80")
	}
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"admin","password":"new-secret"}`))
	r.RemoteAddr = "198.51.100.1:81"
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var response any
	json.Unmarshal(w.Body.Bytes(), &response)
	records = append(records, map[string]any{"case": "login/blocked", "status": w.Code, "response": response, "retry_after": w.Header().Get("Retry-After")})
	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("PRICING_SESSION_CONTRACT %s\n", raw)
}

func BenchmarkAdminSessionPricingHTTP(b *testing.B) {
	st, err := storage.Open(b.TempDir() + "/state.json")
	if err != nil {
		b.Fatal(err)
	}
	st.UpdateSettings(func(settings *storage.Settings) { settings.AdminPassword = "benchmark-fixture" })
	s := newConfiguredServer(st)
	request := httptest.NewRequest("POST", "http://panel.example/auth/login", strings.NewReader(`{"username":"admin","password":"benchmark-fixture"}`))
	w := httptest.NewRecorder()
	s.handleLogin(w, request)
	if w.Code != 200 {
		b.Fatal(w.Code)
	}
	cookie := w.Result().Cookies()[0]
	handler := s.Handler()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest("GET", "http://panel.example/admin/api/pricing", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}
