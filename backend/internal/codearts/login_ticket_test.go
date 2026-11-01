package codearts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func ticketJSON() string {
	return fmt.Sprintf(`{"user_name":"test-user","domain_id":"test-domain","credential":{"access":"ak-test","secret":"sk-test","securitytoken":"token-test","expires_at":%q}}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
}

func loginResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestLoginTicketWaitsForAuthorizationAndHasNoRefresh(t *testing.T) {
	c := NewClient()
	calls := 0
	c.HTTP.Transport = creditRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "snap-access.cn-north-4.myhuaweicloud.com" || r.URL.Path != "/snap-manager/v1/login/ticket" {
			t.Errorf("wrong ticket endpoint: %s", r.URL.Path)
		}
		if r.URL.Query().Get("ticket_id") != "ticket" || r.URL.Query().Get("secret") != "s+/?" {
			t.Error("query encoding lost credential")
		}
		if r.Header.Get("plugin-name") != LoginPluginName || r.Header.Get("plugin-version") != "test-version" {
			t.Error("missing official login headers")
		}
		if calls == 1 {
			return loginResponse(r, `{"error_code":"pending"}`), nil
		}
		return loginResponse(r, ticketJSON()), nil
	})
	s, err := c.ExchangeLoginTicket(t.Context(), "ticket", "s+/?", "test-version", 0)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || s.AccountLabel != "test-user" || s.DomainID != "test-domain" || !s.Valid(time.Minute) || s.HasRefresh() {
		t.Fatalf("incorrect legacy session: calls=%d valid=%v refresh=%v", calls, s.Valid(time.Minute), s.HasRefresh())
	}
}

func TestLoginTicketErrorsDoNotExposeSecrets(t *testing.T) {
	c := NewClient()
	c.HTTP.Transport = creditRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed " + r.URL.String())
	})
	_, err := c.ExchangeLoginTicket(t.Context(), "private-ticket", "private-secret", "", 0)
	if err == nil || strings.Contains(err.Error(), "private-") {
		t.Fatal("ticket secrets leaked through network error")
	}
	for _, body := range []string{`{"credential":{"access":"a","secret":"b"}}`, `{"credential":{"access":"a","secret":"b","securitytoken":"c","expires_at":"2020-01-01T00:00:00Z"}}`} {
		c.HTTP.Transport = creditRoundTripFunc(func(r *http.Request) (*http.Response, error) { return loginResponse(r, body), nil })
		if _, err := c.ExchangeLoginTicket(t.Context(), "t", "s", "", 0); err == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
}

func awaitLogin(t *testing.T, f *LoginFlow) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
		t.Fatal("login did not finish")
	}
}

func TestBrowserTicketCallbackExchangesMatchingTicketOnce(t *testing.T) {
	c := NewClient()
	var calls atomic.Int32
	gate := make(chan struct{})
	defer close(gate)
	var expectedTicket string
	c.HTTP.Transport = creditRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Query().Get("ticket_id") != expectedTicket {
			t.Error("ticket differs from authorize URL")
		}
		select {
		case <-gate:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return loginResponse(r, ticketJSON()), nil
	})
	m := NewLoginManager(c)
	f, err := m.Start(LoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cancel(f.ID)
	u, _ := url.Parse(f.AuthURL)
	expectedTicket = u.Query().Get("ticket_id")
	raw := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback?secret=secret&redirect=%s", f.Port, url.QueryEscape("https://codearts.huaweicloud.com/portal/login"))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for i := 0; i < 2; i++ {
		r, err := client.Get(raw)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 307 {
			t.Fatalf("callback status=%d", r.StatusCode)
		}
	}
	if status, msg, _, _ := f.Snapshot(); status != LoginPending || !strings.Contains(msg, "票据") {
		t.Fatalf("callback status not observable: %s %s", status, msg)
	}
	// Cancel the live worker and assert no duplicate exchange or retry survives.
	m.Cancel(f.ID)
	awaitLogin(t, f)
	if calls.Load() > 1 {
		t.Fatalf("duplicate callback exchanged %d times", calls.Load())
	}
}

func TestPastedTicketCallbackCompletesWithoutRedirectFetch(t *testing.T) {
	c := NewClient()
	c.HTTP.Transport = creditRoundTripFunc(func(r *http.Request) (*http.Response, error) { return loginResponse(r, ticketJSON()), nil })
	m := NewLoginManager(c)
	f, err := m.Start(LoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cancel(f.ID)
	raw := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback?secret=secret&redirect=https://example.invalid/should-not-fetch", f.Port)
	if _, err := f.SubmitCallbackURL(raw); err != nil {
		t.Fatal(err)
	}
	awaitLogin(t, f)
	if status, _, _, s := f.Snapshot(); status != LoginConfirmed || s == nil || s.HasRefresh() {
		t.Fatal("pasted ticket did not produce temporary credentials")
	}
	if _, err := f.SubmitCallbackURL(raw); err != nil {
		t.Fatal("completed flow rejected the same callback")
	}
}

func TestOAuthCallbackReportsPendingAndRejectsDuplicateExchange(t *testing.T) {
	c := NewClient()
	var tokenCalls atomic.Int32
	gate := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(gate)
		}
	}()
	c.HTTP.Transport = creditRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v5/caller-identity" {
			return loginResponse(r, `{"user":{"id":"u","name":"User","domain_id":"d"}}`), nil
		}
		tokenCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("code") != "oauth-code" || r.Form.Get("grant_type") != "authorization_code" || r.Header.Get("DPoP") == "" {
			t.Error("OAuth exchange malformed")
		}
		select {
		case <-gate:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return loginResponse(r, fmt.Sprintf(`{"credentials":{"access_key_id":"ak","secret_access_key":"sk","security_token":"st","expiration":%q},"refresh_token":"refresh"}`, time.Now().Add(time.Hour).Format(time.RFC3339))), nil
	})
	m := NewLoginManager(c)
	f, err := m.Start(LoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cancel(f.ID)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://127.0.0.1/oauth/callback?code=oauth-code", nil)
		f.handleCallback(LoginOptions{PortalHost: DefaultPortalHost})(w, r)
		if w.Code != 200 || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), "查看最终结果") {
			t.Fatal("callback prematurely announced login success")
		}
	}
	close(gate)
	released = true
	awaitLogin(t, f)
	if tokenCalls.Load() != 1 {
		t.Fatal("authorization code exchanged more than once")
	}
	status, _, _, sess := f.Snapshot()
	if status != LoginConfirmed || sess == nil || !sess.HasRefresh() {
		t.Fatal("OAuth credentials cannot be refreshed")
	}
	pasted := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback?code=oauth-code", f.Port)
	if _, err := f.SubmitCallbackURL(pasted); err != nil {
		t.Fatalf("browser callback then pasted callback failed: %v", err)
	}
	if tokenCalls.Load() != 1 {
		t.Fatal("completed authorization exchanged twice")
	}
	encoded, _ := json.Marshal(sess)
	if len(encoded) == 0 {
		t.Fatal("session not serializable")
	}
}

func TestPastedCallbackRejectsWrongSessionPort(t *testing.T) {
	f := &LoginFlow{Port: 12345}
	for _, raw := range []string{"http://127.0.0.1/oauth/callback?secret=x", "http://127.0.0.1:12346/oauth/callback?secret=x", "https://codearts.huaweicloud.com/portal/authorize"} {
		if _, err := f.SubmitCallbackURL(raw); err == nil {
			t.Fatal("accepted callback from another login")
		}
	}
}
