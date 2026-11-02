package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/codearts"
	"codearts2api/internal/registry"
	"codearts2api/internal/storage"
)

type lifecycleTransport func(*http.Request) (*http.Response, error)

func (f lifecycleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the actual app listener and gateway while Close is draining HTTP.
// No request leaves this process; the transport is a controlled SSE producer.
func TestRuntimeDrainsStreamAndPersistsFinalUsage(t *testing.T) {
	st, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateSettings(func(s *storage.Settings) {
		s.AutoClaim, s.AutoRefresh, s.WriteBackToIDE = false, false, false
		s.RateLimitEnabled, s.AdminPassword = false, "fixture-password"
	})
	st.UpsertAccount(&accounts.Account{ID: "fixture", Enabled: true, NativeCredential: &codearts.Session{
		AccessKey: "fixture-ak", SecretKey: "fixture-sk", ExpiresAt: time.Now().Add(time.Hour),
	}})
	key := st.AddKey("fixture").Key
	srv, err := Build(st)
	if err != nil {
		t.Fatal(err)
	}
	srv.Reg.Replace([]registry.Model{{ID: "test-model", StreamOnly: true}})
	release := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(finish)
	requestContext := make(chan context.Context, 1)
	srv.Up.HTTP = &http.Client{Transport: lifecycleTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != codearts.ChatPath {
			return nil, fmt.Errorf("fixture catalog unavailable")
		}
		requestContext <- r.Context()
		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"fixture\"}}]}\n\n")
			<-release
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"-complete\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4,\"total_tokens\":16}}\n\ndata: [DONE]\n\n")
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader, Request: r}, nil
	})}
	portProbe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portProbe.Addr().(*net.TCPAddr).Port
	_ = portProbe.Close()
	running := Start(st, srv, Options{Host: "127.0.0.1", Port: port, Version: "fixture"})
	closeLogs := make(chan string, 8)
	running.logf = func(format string, args ...any) { closeLogs <- fmt.Sprintf(format, args...) }
	var closeOnce sync.Once
	closeRuntime := func() { closeOnce.Do(running.Close) }
	t.Cleanup(func() { finish(); closeRuntime() })
	httpServer := running.http.(*http.Server)
	if httpServer.ReadHeaderTimeout != 20*time.Second || httpServer.WriteTimeout != 0 || httpServer.BaseContext != nil {
		t.Fatal("HTTP timeouts or request cancellation context changed")
	}
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 5 * time.Second, Transport: transport}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get(base + "/health")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener not ready", err)
		}
		time.Sleep(time.Millisecond)
	}
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/chat/completions", strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	req.Header.Set("Authorization", "Bearer "+key)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(response.Body)
		t.Fatal(response.StatusCode, string(raw))
	}
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(first, "fixture") || st.PoolSnapshot()["fixture"].InFlight != 1 {
		t.Fatal("stream did not start", first, err)
	}
	upstreamContext := <-requestContext
	closed := make(chan struct{})
	go func() { closeRuntime(); close(closed) }()
	// Shutdown must stop accepting new connections while the old stream drains.
	probeTransport := &http.Transport{DisableKeepAlives: true}
	defer probeTransport.CloseIdleConnections()
	probe := &http.Client{Transport: probeTransport, Timeout: time.Second}
	for {
		response, err := probe.Get(base + "/health")
		if err != nil {
			break
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if time.Now().After(deadline) {
			t.Fatal("shutdown did not close the listener")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-closed:
		t.Fatal("Close returned before the active stream completed")
	default:
	}
	if upstreamContext.Err() != nil {
		t.Fatal("shutdown canceled a valid active request")
	}
	finish()
	rest, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(rest), "-complete") || !strings.HasSuffix(strings.TrimSpace(string(rest)), "data: [DONE]") {
		t.Fatal("stream was truncated during shutdown", string(rest), err)
	}
	select {
	case <-closed:
	case <-time.After(12 * time.Second):
		t.Fatal("Close did not finish after the stream completed")
	}
	for len(closeLogs) > 0 {
		if message := <-closeLogs; strings.Contains(message, "异常") || strings.Contains(message, "失败") {
			t.Fatal("shutdown exceeded its original deadline or failed to save", message)
		}
	}
	if st.PoolSnapshot()["fixture"].InFlight != 0 {
		t.Fatal("lease leaked at shutdown")
	}
	restored, err := storage.Open(st.Path())
	if err != nil {
		t.Fatal(err)
	}
	logs := restored.Logs(1)
	if len(logs) != 1 || logs[0].Status != 200 || logs[0].FinishReason != "stop" || !logs[0].Usage.Known || logs[0].Usage.InputTokens != 12 || logs[0].Usage.OutputTokens != 4 {
		t.Fatal("final usage was not persisted", logs)
	}
}
