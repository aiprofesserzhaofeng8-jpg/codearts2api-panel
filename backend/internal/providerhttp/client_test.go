package providerhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPKCECallbackBindingAndOneTimeRedemption(t *testing.T) {
	p := &PKCE{Config: OAuthConfig{Name: "fixture", ClientID: "public-client", Authorize: "https://example.com/auth", Redirect: "http://localhost:1455/auth/callback", Scopes: "openid offline_access"}}
	login, e := p.Begin(context.Background(), "global")
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(login.URL)
	q := u.Query()
	if q.Get("state") != login.State || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 {
		t.Fatal("missing PKCE binding")
	}
	var calls atomic.Int32
	exchange := func(_ context.Context, code, verifier string) (json.RawMessage, error) {
		calls.Add(1)
		if code != "fixture" || len(verifier) != 43 {
			t.Error("bad exchange")
		}
		return json.RawMessage(`{"private":"fixture-token"}`), nil
	}
	for _, bad := range []string{"http://evil.example/auth/callback?code=fixture&state=" + login.State, "http://localhost:1455/auth/callback?code=fixture&code=duplicate&state=" + login.State, "fixture#foreign", "fixture"} {
		if _, _, e = p.Check(context.Background(), "global", login.State, bad, exchange); e == nil {
			t.Fatal("accepted unrelated callback", bad)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid state redeemed")
	}
	callback := "http://localhost:1455/auth/callback?code=fixture&state=" + login.State
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, ready, e := p.Check(context.Background(), "global", login.State, callback, exchange)
			if e != nil || !ready || !strings.Contains(string(raw), "fixture-token") {
				t.Error("lost redeemed credentials", e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("authorization code replayed", calls.Load())
	}
	p.Release(login.State)
	if _, _, e = p.Check(context.Background(), "global", login.State, callback, exchange); e == nil {
		t.Fatal("canceled login revived")
	}
}
func TestHTTPDoesNotRedirectCredentialsAndSanitizesErrors(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer target.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer up.Close()
	response, e := Do(context.Background(), New(), "fixture", http.MethodPost, up.URL, http.Header{"Authorization": {"Bearer private-fixture"}}, map[string]string{"code": "private-code"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = JSON(response, "fixture")
	if e == nil || leaked || strings.Contains(e.Error(), "private") {
		t.Fatal("redirect leaked credential", e)
	}
	response = &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"error":"private-secret"}`))}
	_, e = JSON(response, "fixture")
	if e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal("upstream error exposed")
	}
	response = &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[],"error":null}`))}
	if _, e = JSON(response, "fixture"); e != nil {
		t.Fatal("null error rejected", e)
	}
}
func TestSSEAuthoritativeEndAndCancellation(t *testing.T) {
	consume := func(event string, b []byte) error {
		if event == "done" && string(b) == "{}" {
			return End
		}
		return nil
	}
	for _, input := range []string{"", "data: {}\n\n", "event: done\ndata: {}"} {
		if e := SSE(context.Background(), strings.NewReader(input), "fixture", consume); e == nil {
			t.Fatal("EOF marked completed")
		}
	}
	if e := SSE(context.Background(), strings.NewReader(": heartbeat\n\nevent: done\ndata: {}\n\n"), "fixture", consume); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := SSE(ctx, strings.NewReader("data: {}\n\n"), "fixture", consume); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestCallerCancellationInterruptsOpenStream(t *testing.T) {
	disconnected := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(disconnected)
	}))
	defer up.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response, e := Do(ctx, New(), "fixture", http.MethodPost, up.URL, nil, map[string]string{"model": "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	e = SSE(ctx, response.Body, "fixture", func(string, []byte) error { cancel(); return nil })
	if !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation did not interrupt read", e)
	}
	select {
	case <-disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request remained alive")
	}
}
