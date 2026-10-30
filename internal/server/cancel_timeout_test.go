package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/ratelimit"
	"codearts2api/internal/store"
)

const interruptionBeforeHeaders = -1

// Both HTTP hops and each provider's native client are real. Only destinations
// and credentials are fixtures; production routing, leases and retries run.
func interruptionServer(t *testing.T, upstream string, handler http.HandlerFunc) (*Server, string, string, string) {
	t.Helper()
	var s *Server
	var key, model, account string
	switch upstream {
	case "workbuddy":
		s, key, _ = nativeWBServer(t, handler)
		addWB(t, s, "cn", time.Now().Add(time.Hour).Unix())
		model, account = "workbuddy/cn/shared", "wb-cn"
	case "trae":
		s, key, _ = nativeTraeServer(t, handler)
		addTrae(t, s)
		model, account = "trae/cn/shared", "trae-acct"
	default:
		s = poolServer(t, 1)
		key, model, account = s.Store.AddKey("interruption").Key, "test-model", "a"
		up := httptest.NewServer(handler)
		t.Cleanup(up.Close)
		target, _ := url.Parse(up.URL)
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		t.Cleanup(transport.CloseIdleConnections)
		s.Up.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			cloned := r.Clone(r.Context())
			cloned.URL.Scheme, cloned.URL.Host, cloned.Host = target.Scheme, target.Host, target.Host
			return transport.RoundTrip(cloned)
		})}
	}
	s.Store.UpdateSettings(func(c *store.Settings) {
		c.MaxRetries = 3
		c.Pool.MaxInFlight = 1
		c.RequestTimeout = 1
	})
	// A recovery request must also reacquire the same per-key concurrency slot.
	s.Limits.SetConfig(ratelimit.Config{Enabled: true, QPS: 100, Burst: 100, Concurrency: 1})
	return s, key, model, account
}

func interruptionPayload(upstream, text string, complete bool) string {
	if upstream == "trae" {
		body := fmt.Sprintf("event: output\ndata: {\"response\":%q}\n\n", text)
		if complete {
			body += "event: token_usage\ndata: {\"prompt_tokens\":3,\"completion_tokens\":1}\n\nevent: done\ndata: {\"finish_reason\":\"stop\"}\n\n"
		}
		return body
	}
	if complete {
		return summaryFixture(text, "stop", 3, 1)
	}
	return fmt.Sprintf("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", text)
}

func awaitInterruption(t *testing.T, event <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for " + name)
	}
}

func runHTTPInterruption(t *testing.T, upstream, path string, stream bool, cause string, errorStatus int) {
	t.Helper()
	var calls atomic.Int32
	entered, canceled, cleanup := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s, key, model, account := interruptionServer(t, upstream, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) > 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, interruptionPayload(upstream, "RECOVERED", true))
			return
		}
		if errorStatus > 0 {
			w.WriteHeader(errorStatus)
			w.(http.Flusher).Flush()
		} else if errorStatus == 0 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, interruptionPayload(upstream, "INTERRUPTION_PROBE", false))
			w.(http.Flusher).Flush()
		}
		close(entered)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-cleanup:
		}
	})
	t.Cleanup(func() { close(cleanup) })
	original := s.Store.Account(account)
	done := make(chan struct{}, 2)
	h := s.Handler()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cause == "deadline" && r.Header.Get("X-Test-Interrupt") == "yes" {
			// Exercise the same inherited server deadline at 100ms; the separate
			// configured_timeout case exercises the actual 1-second setting.
			ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
			defer cancel()
			r = r.WithContext(ctx)
		}
		h.ServeHTTP(w, r)
		done <- struct{}{}
	}))
	defer gateway.Close()
	client := gateway.Client()
	client.Timeout = 3 * time.Second
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := fmt.Sprintf(`{"model":%q,"stream":%t,"max_tokens":32,"messages":[{"role":"user","content":"hello"}],"input":"hello"}`, model, stream)
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Interrupt", "yes")
	type result struct {
		status int
		body   string
		err    error
	}
	resultCh := make(chan result, 1)
	visible := make(chan struct{})
	go func() {
		resp, err := client.Do(request)
		if err != nil {
			resultCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var body strings.Builder
		reader := bufio.NewReader(resp.Body)
		var once sync.Once
		for {
			line, err := reader.ReadString('\n')
			body.WriteString(line)
			if strings.Contains(line, "INTERRUPTION_PROBE") {
				once.Do(func() { close(visible) })
			}
			if err != nil {
				resultCh <- result{resp.StatusCode, body.String(), err}
				return
			}
		}
	}()
	awaitInterruption(t, entered, "upstream request")
	if stream && errorStatus == 0 && cause == "cancel" {
		awaitInterruption(t, visible, "client-visible stream output")
	}
	if cause == "cancel" {
		cancel()
	}
	select {
	case got := <-resultCh:
		if cause != "cancel" {
			if errorStatus == 0 && stream {
				if got.status != 200 || !strings.Contains(got.body, "INTERRUPTION_PROBE") || !strings.Contains(got.body, "error") && !strings.Contains(got.body, "response.failed") || strings.Contains(got.body, "message_stop") || strings.Contains(got.body, "response.completed") {
					t.Fatalf("timeout manufactured success or dropped partial output: %+v", got)
				}
			} else if got.status < 400 {
				t.Fatalf("timeout counted as HTTP success: %+v", got)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interrupted request did not return")
	}
	awaitInterruption(t, canceled, "upstream socket cancellation")
	awaitInterruption(t, done, "gateway handler completion")
	a := s.Store.Account(account)
	health := s.Store.ModelHealthSnapshot()[account][model]
	pool := s.Store.PoolSnapshot()[account]
	if calls.Load() != 1 || a.Failures != original.Failures || a.Status != original.Status || a.CooldownUntil != original.CooldownUntil || health.Failures != 0 || health.CooldownUntil != 0 || pool.InFlight != 0 {
		t.Fatalf("interruption replayed, penalized account/model, or leaked lease: calls=%d account_status=%q account_failures=%d account_cooldown=%d model=%+v pool=%+v", calls.Load(), a.Status, a.Failures, a.CooldownUntil, health, pool)
	}
	log := s.Store.Logs(1)[0]
	if log.Attempts != 1 || log.Status < 400 || log.Usage.Known || s.Store.Stats().FailedRequests != 1 {
		t.Fatalf("interruption accounting invalid: %+v", log)
	}
	wantKind := "timeout"
	if cause == "cancel" {
		wantKind = "canceled"
	}
	if log.ErrorKind != wantKind || len(log.AttemptDetails) != 1 || log.AttemptDetails[0].ErrorKind != wantKind || log.AttemptDetails[0].DurationMS == nil {
		t.Fatalf("interruption timing/classification invalid: %+v", log)
	}
	// A second request with the same key/model must succeed immediately. This
	// catches leaked account leases, rate-limit permits and hidden model cooldown.
	recovery, _ := http.NewRequest(http.MethodPost, gateway.URL+path, strings.NewReader(body))
	recovery.Header.Set("Authorization", "Bearer "+key)
	recovery.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(recovery)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 || !strings.Contains(string(raw), "RECOVERED") || calls.Load() != 2 {
		t.Fatalf("recovery failed: status=%d calls=%d err=%v body=%s", resp.StatusCode, calls.Load(), err, raw)
	}
	awaitInterruption(t, done, "recovery handler completion")
	if p := s.Store.PoolSnapshot()[account]; p.InFlight != 0 {
		t.Fatal("recovery leaked account lease")
	}
}

func TestHTTPInterruptionReleasesAndRecovers(t *testing.T) {
	for _, upstream := range []string{"codearts", "workbuddy", "trae"} {
		for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
			for _, stream := range []bool{false, true} {
				for _, cause := range []string{"cancel", "deadline"} {
					t.Run(fmt.Sprintf("%s/%s/stream=%t/%s", upstream, path, stream, cause), func(t *testing.T) {
						runHTTPInterruption(t, upstream, path, stream, cause, 0)
					})
				}
			}
		}
	}
}

func TestHTTPConfiguredTimeoutReleasesAndRecovers(t *testing.T) {
	for _, upstream := range []string{"codearts", "workbuddy", "trae"} {
		t.Run(upstream, func(t *testing.T) {
			runHTTPInterruption(t, upstream, "/v1/chat/completions", true, "configured_timeout", 0)
		})
	}
}

func TestHTTPCanceledProviderErrorBodyDoesNotPenalize(t *testing.T) {
	for _, upstream := range []string{"codearts", "workbuddy", "trae"} {
		for _, status := range []int{401, 429} {
			for _, cause := range []string{"cancel", "deadline"} {
				t.Run(fmt.Sprintf("%s/%d/%s", upstream, status, cause), func(t *testing.T) {
					runHTTPInterruption(t, upstream, "/v1/chat/completions", false, cause, status)
				})
			}
		}
	}
}

func TestHTTPInterruptionBeforeHeadersReleasesAndRecovers(t *testing.T) {
	for _, upstream := range []string{"codearts", "workbuddy", "trae"} {
		for _, cause := range []string{"cancel", "deadline"} {
			t.Run(upstream+"/"+cause, func(t *testing.T) {
				runHTTPInterruption(t, upstream, "/v1/chat/completions", false, cause, interruptionBeforeHeaders)
			})
		}
	}
}

func TestHTTPProviderErrorsWithoutCancellationStillClassify(t *testing.T) {
	for _, upstream := range []string{"workbuddy", "trae"} {
		for _, status := range []int{401, 429} {
			t.Run(fmt.Sprintf("%s/%d", upstream, status), func(t *testing.T) {
				var calls atomic.Int32
				s, key, model, account := interruptionServer(t, upstream, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{}`)
				})
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, model)
				w := responseRequest(s, key, "/v1/chat/completions", body)
				a := s.Store.Account(account)
				health := s.Store.ModelHealthSnapshot()[account][model]
				if status == 401 && (a.Failures != 1 || a.CooldownUntil <= time.Now().Unix()) {
					t.Fatal("real credential rejection no longer marks/cools account")
				}
				if status == 429 && (a.Failures != 0 || health.Failures != 1 || health.CooldownUntil <= time.Now().Unix()) {
					t.Fatal("real rate limit no longer cools only the model")
				}
				if calls.Load() != 1 || w.Code < 400 || s.Store.PoolSnapshot()[account].InFlight != 0 {
					t.Fatalf("error request replayed, reported success or leaked lease: calls=%d status=%d", calls.Load(), w.Code)
				}
			})
		}
	}
}
