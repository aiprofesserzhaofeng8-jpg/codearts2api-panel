package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

func poolServer(t *testing.T, n int) *Server {
	s := newTestServer(t, n, 3)
	s.Store.UpdateSettings(func(c *store.Settings) { c.Pool = store.DefaultPoolSettings() })
	s = New(s.Store)
	s.AllowLocalAdmin = true
	s.Reg.Replace([]registry.Model{{ID: "test-model", StreamOnly: true}})
	for _, a := range s.Store.Accounts() {
		sess := *a.Session
		sess.ExpiresAt = time.Now().Add(time.Hour)
		s.Store.SetAccountSession(a.ID, &sess)
	}
	return s
}

func TestHTTPPoolPreservesPromptToolsAndSignedRequest(t *testing.T) {
	s := poolServer(t, 1)
	key := s.Store.AddKey("test").Key
	s.Store.UpdateSettings(func(c *store.Settings) { c.AdminPassword = "administrator" })
	var received map[string]any
	s.Up.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "SDK-HMAC-SHA256 ") {
			t.Error("Huawei request must be signed")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sseFixture)), Request: r}, nil
	})}
	body := `{"model":"test-model","messages":[{"role":"system","content":"keep my project rules"},{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}],"stream":false}`
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "你好世界") {
		t.Fatalf("invalid completion: %d %s", w.Code, w.Body.String())
	}
	messages := received["messages"].([]any)
	if messages[0].(map[string]any)["content"] != "keep my project rules" || len(received["tools"].([]any)) != 1 {
		t.Fatal("client instructions or tools were altered")
	}
	if s.Store.PoolSnapshot()["a"].InFlight != 0 {
		t.Fatal("blocking request leaked lease")
	}
	for _, path := range []string{"/admin/api/state", "/admin/api/settings"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("API key accessed admin endpoint: %s status=%d", path, w.Code)
		}
	}
}

func TestHTTPStreamFailureNeverReplaysVisibleOutput(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			s := poolServer(t, 2)
			calls := 0
			s.Up.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				payload := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"only-once\"}}]}\n\ndata: {\"error_code\":\"TM.00001041\",\"error_msg\":\"concurrency limit\"}\n\n"
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
			})}
			r := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"test-model","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`))
			r.RemoteAddr = "127.0.0.1:1234"
			r.Host = "localhost"
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 200 || calls != 1 || strings.Count(w.Body.String(), "only-once") != 1 || !strings.Contains(w.Body.String(), "error") {
				t.Fatalf("stream replayed or error lost: calls=%d status=%d body=%s", calls, w.Code, w.Body.String())
			}
			for _, p := range s.Store.PoolSnapshot() {
				if p.InFlight != 0 {
					t.Fatal("stream leaked lease")
				}
			}
		})
	}
}

func TestPoolRateLimitRotatesAndPersistsCooldown(t *testing.T) {
	s := poolServer(t, 2)
	oldWait := waitRetry
	waitRetry = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { waitRetry = oldWait })
	var seen []string
	_, err := s.withAccountRetry(t.Context(), time.Second, func(_ context.Context, a *codearts.Account) error {
		seen = append(seen, a.ID)
		if len(seen) == 1 {
			return transientErr()
		}
		return nil
	})
	if err != nil || len(seen) != 2 || seen[0] == seen[1] {
		t.Fatalf("failover failed: %v %v", seen, err)
	}
	if s.Store.PoolSnapshot()[seen[0]].State != "cooling" {
		t.Fatal("rate limited account not cooling")
	}
	reopened, err := store.Open(s.Store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.PoolSnapshot()[seen[0]].State != "cooling" {
		t.Fatal("cooldown was not saved")
	}
	if s.Store.Account(seen[0]).Failures != 0 {
		t.Fatal("rate limit incorrectly counted as account failure")
	}
}

func TestPoolCapacityAndCancellation(t *testing.T) {
	s := poolServer(t, 1)
	_, a := s.Store.AcquireAccount("", nil)
	defer a()
	_, b := s.Store.AcquireAccount("", nil)
	defer b()
	_, err := s.withAccountRetry(t.Context(), time.Second, func(context.Context, *codearts.Account) error { t.Fatal("pool capacity exceeded"); return nil })
	if !errors.Is(err, apierr.NoAccount) {
		t.Fatalf("saturated pool must return 503-compatible error: %v", err)
	}
	a()
	b()
	ctx, cancel := context.WithCancel(t.Context())
	_, _ = s.withAccountRetry(ctx, time.Second, func(context.Context, *codearts.Account) error { cancel(); return context.Canceled })
	if p := s.Store.PoolSnapshot()["a"]; p.InFlight != 0 || p.State != "ready" {
		t.Fatalf("canceled client penalized account: %+v", p)
	}
}

func TestRoutingSessionIsolationAndMultiTurn(t *testing.T) {
	keyFor := func(key, id, body string) string {
		r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("X-Session-Id", id)
		v, _ := withRoutingSession(r, []byte(body)).Value(routingSessionKey{}).(string)
		return v
	}
	body := `{"model":"m","messages":[{"role":"user","content":"first"}]}`
	next := `{"model":"m","messages":[{"role":"user","content":"first"},{"role":"assistant","content":"reply"},{"role":"user","content":"next"}]}`
	if keyFor("a", "", body) != keyFor("a", "", next) {
		t.Fatal("multi-turn key changed")
	}
	if keyFor("a", "shared", body) == keyFor("b", "shared", body) {
		t.Fatal("different API keys share a session")
	}
	if keyFor("a", "one", body) == keyFor("a", "two", body) {
		t.Fatal("explicit session IDs ignored")
	}
}

func TestPoolSettingsValidationAndPauseHTTP(t *testing.T) {
	s := poolServer(t, 1)
	postSettings(t, s, `{"pool":{"strategy":"round_robin","max_in_flight":1},"auto_claim":false,"claim_after_hour":7}`)
	if cfg := s.Store.Settings(); cfg.Pool.MaxInFlight != 1 || cfg.Pool.StickyTTLSec != 1800 || cfg.AutoClaim || cfg.ClaimAfterHour != 7 {
		t.Fatalf("partial settings not applied correctly: %+v", cfg.Pool)
	}
	r := httptest.NewRequest("POST", "/admin/api/settings", strings.NewReader(`{"pool":{"max_in_flight":0},"default_model":"must-not-save"}`))
	w := httptest.NewRecorder()
	s.handleAdminSettings(w, r)
	if w.Code != 400 || s.Store.Settings().DefaultModel == "must-not-save" {
		t.Fatal("invalid settings were partially persisted")
	}
	for _, action := range []string{"pause", "resume"} {
		r := httptest.NewRequest("POST", "/admin/api/accounts", strings.NewReader(`{"action":"`+action+`","id":"a"}`))
		w := httptest.NewRecorder()
		s.handleAdminAccounts(w, r)
		if w.Code != 200 || s.Store.Account("a").Paused != (action == "pause") {
			t.Fatalf("%s failed: %s", action, w.Body.String())
		}
	}
}
