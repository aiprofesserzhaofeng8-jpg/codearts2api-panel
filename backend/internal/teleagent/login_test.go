package teleagent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func loginResult(challenge string, credentials any) string {
	return string(raw(map[string]any{"version": 1, "provider": "teleagent", "challenge": challenge, "credentials": credentials}))
}

func TestBrowserHelperBindsFlowAndVerifiesBeforePreview(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != UserPath || r.Method != "GET" || r.Header.Get("X-Token") != fixture().Token || r.Header.Get("Authorization") != "" {
			t.Error("wrong login verification")
		}
		io.WriteString(w, `{"code":0,"data":{"accountId":"fixture-account","accountName":"Verified name"}}`)
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	flow, err := c.BeginLogin(context.Background(), "cn")
	if err != nil || flow.URL != LoginURL || flow.State != flow.HelperChallenge || len(flow.State) != 43 {
		t.Fatal("bad login task", err)
	}
	if _, ready, err := c.CheckLogin(context.Background(), "cn", flow.State); err != nil || ready {
		t.Fatal("empty login was completed")
	}
	for _, input := range []string{
		loginResult("wrong", fixture()),
		loginResult(flow.State, map[string]string{"token": fixture().Token}),
		`{"version":1,"provider":"teleagent","challenge":"` + flow.State + `","credentials":{},"extra":true}`,
		loginResult(flow.State, fixture()) + `{}`,
		`https://agent.teleai.com.cn/superCowork/sapi/cowork/auth?code=used-code`,
	} {
		if _, ready, err := c.CheckCallback(context.Background(), "cn", flow.State, input); err == nil || ready {
			t.Fatal("unbound or incomplete result accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid results reached upstream")
	}
	input := loginResult(flow.State, fixture())
	b, ready, err := c.CheckCallback(context.Background(), "cn", flow.State, input)
	if err != nil || !ready || calls != 1 {
		t.Fatal("no verified preview", err)
	}
	var a Credentials
	_ = json.Unmarshal(b, &a)
	if a.Name != "Verified name" || a.InstallID != fixture().InstallID {
		t.Fatal("identity or device lost")
	}
	// Concurrent retries share one verified result and never consume the code again.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, ok, e := c.CheckCallback(context.Background(), "cn", flow.State, input); e != nil || !ok {
				t.Error("idempotence failed")
			}
		})
	}
	wg.Wait()
	if calls != 1 {
		t.Fatal("repeated upstream verification")
	}
	other := fixture()
	other.DeviceID = "other-device"
	if _, _, err = c.CheckCallback(context.Background(), "cn", flow.State, loginResult(flow.State, other)); err == nil {
		t.Fatal("verified result changed")
	}
	c.ReleaseLogin(flow.State)
	if _, _, err = c.CheckCallback(context.Background(), "cn", flow.State, input); err == nil {
		t.Fatal("released login reused")
	}
}

func TestBrowserHelperRejectsWrongIdentityAndExpiry(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":0,"data":{"accountId":"other"}}`)
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	f, _ := c.BeginLogin(context.Background(), "cn")
	if _, _, err := c.CheckCallback(context.Background(), "cn", f.State, loginResult(f.State, fixture())); err == nil {
		t.Fatal("wrong identity accepted")
	}
	c.loginMu.Lock()
	c.logins[f.State].created = time.Now().Add(-11 * time.Minute)
	c.loginMu.Unlock()
	if _, _, err := c.CheckCallback(context.Background(), "cn", f.State, ""); err == nil {
		t.Fatal("expired login pending")
	}
	if _, err := c.BeginLogin(context.Background(), "global"); err == nil {
		t.Fatal("global accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.BeginLogin(ctx, "cn"); err == nil {
		t.Fatal("cancellation lost")
	}
	if _, _, err := c.CheckCallback(ctx, "cn", f.State, ""); err == nil {
		t.Fatal("cancellation lost")
	}
	f, _ = c.BeginLogin(context.Background(), "cn")
	if _, _, err := c.CheckCallback(context.Background(), "cn", f.State, strings.Repeat("x", 257<<10)); err == nil {
		t.Fatal("oversized result accepted")
	}
}

func TestBrowserHelperCancelDuringVerification(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-finish
		io.WriteString(w, `{"code":0,"data":{"accountId":"fixture-account"}}`)
	}))
	defer up.Close()
	c := New()
	c.Origin = up.URL
	f, _ := c.BeginLogin(context.Background(), "cn")
	done := make(chan error, 1)
	go func() {
		_, _, err := c.CheckCallback(context.Background(), "cn", f.State, loginResult(f.State, fixture()))
		done <- err
	}()
	<-entered
	c.ReleaseLogin(f.State)
	close(finish)
	if err := <-done; err == nil {
		t.Fatal("canceled verification returned credentials")
	}
}
