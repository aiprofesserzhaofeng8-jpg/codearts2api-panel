package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type connectionFixture struct {
	header                 http.Header
	deadlines              []time.Time
	readDeadlines          []time.Time
	writeError, flushError error
}

func (w *connectionFixture) Header() http.Header { return w.header }
func (*connectionFixture) WriteHeader(int)       {}
func (w *connectionFixture) Write(b []byte) (int, error) {
	if w.writeError != nil {
		return 0, w.writeError
	}
	return len(b), nil
}
func (w *connectionFixture) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}
func (w *connectionFixture) SetReadDeadline(deadline time.Time) error {
	w.readDeadlines = append(w.readDeadlines, deadline)
	return nil
}
func (w *connectionFixture) Flush()            {}
func (w *connectionFixture) FlushError() error { return w.flushError }

func TestResponseConnectionCancelsOnWriteAndFlushFailure(t *testing.T) {
	for _, flush := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		f := &connectionFixture{header: make(http.Header)}
		w, finish := boundResponseConnection(f, cancel, time.Second)
		w.WriteHeader(200)
		if _, err := w.Write([]byte("first")); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatal("healthy write canceled request")
		}
		if flush {
			f.flushError = errors.New("write timeout")
			w.(http.Flusher).Flush()
		} else {
			f.writeError = errors.New("broken pipe")
			_, _ = w.Write([]byte("later"))
		}
		if ctx.Err() != context.Canceled {
			t.Fatal("failed connection did not cancel upstream")
		}
		if len(f.deadlines) < 3 || f.deadlines[0].IsZero() || f.deadlines[len(f.deadlines)-1].Before(f.deadlines[0]) {
			t.Fatal("write deadline not renewed")
		}
		finish()
		if !f.deadlines[len(f.deadlines)-1].IsZero() {
			t.Fatal("deadline retained on keepalive connection")
		}
		cancel()
	}
}

func TestRequestBodyDeadlineClearedBeforeModelGeneration(t *testing.T) {
	f := &connectionFixture{header: make(http.Header)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &connectionBody{ReadCloser: io.NopCloser(strings.NewReader("request")), controller: http.NewResponseController(f), cancel: cancel, deadline: time.Second}
	b, err := io.ReadAll(body)
	if err != nil || string(b) != "request" || ctx.Err() != nil {
		t.Fatal("normal request body failed")
	}
	if len(f.readDeadlines) < 2 || !f.readDeadlines[len(f.readDeadlines)-1].IsZero() {
		t.Fatal("upload deadline leaked into model generation")
	}
	_ = body.Close()
}

func TestRealConnectionStalledUploadAndDownloadAreBounded(t *testing.T) {
	for _, upload := range []bool{true, false} {
		t.Run(map[bool]string{true: "upload", false: "download"}[upload], func(t *testing.T) {
			done := make(chan bool, 1)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				writer, finish := boundResponseConnection(w, cancel, 100*time.Millisecond)
				defer finish()
				if upload {
					body := &connectionBody{ReadCloser: r.Body, controller: http.NewResponseController(w), cancel: cancel, deadline: 100 * time.Millisecond}
					_, err := io.ReadAll(body)
					done <- err != nil && ctx.Err() != nil
					return
				}
				chunk := make([]byte, 64<<10)
				for range 1024 {
					if _, err := writer.Write(chunk); err != nil {
						done <- ctx.Err() != nil
						return
					}
				}
				done <- false
			}))
			defer up.Close()
			conn, err := net.DialTimeout("tcp", strings.TrimPrefix(up.URL, "http://"), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetReadBuffer(1024)
			}
			request := "GET / HTTP/1.1\r\nHost: fixture\r\n\r\n"
			if upload {
				request = "POST / HTTP/1.1\r\nHost: fixture\r\nContent-Length: 50\r\n\r\nx"
			}
			if _, err = io.WriteString(conn, request); err != nil {
				t.Fatal(err)
			}
			select {
			case ok := <-done:
				if !ok {
					t.Fatal("stalled connection did not cancel")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stalled connection kept request alive")
			}
		})
	}
}
