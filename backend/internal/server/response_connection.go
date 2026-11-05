package server

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Renew the deadline for each individual downstream write. Long model thinking
// is unaffected, while a client that stops reading cannot hold account/key
// concurrency forever. A failed write cancels the same upstream request.
type connectionWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	cancel     context.CancelFunc
	deadline   time.Duration
}

type connectionBody struct {
	io.ReadCloser
	controller *http.ResponseController
	cancel     context.CancelFunc
	deadline   time.Duration
}

func (b *connectionBody) Read(p []byte) (int, error) {
	_ = b.controller.SetReadDeadline(time.Now().Add(b.deadline))
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		_ = b.controller.SetReadDeadline(time.Time{})
		if err != io.EOF {
			b.cancel()
		}
	}
	return n, err
}
func (b *connectionBody) Close() error {
	_ = b.controller.SetReadDeadline(time.Time{})
	return b.ReadCloser.Close()
}

func (w *connectionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *connectionWriter) prepare()                    { _ = w.controller.SetWriteDeadline(time.Now().Add(w.deadline)) }
func (w *connectionWriter) WriteHeader(status int)      { w.prepare(); w.ResponseWriter.WriteHeader(status) }
func (w *connectionWriter) Write(p []byte) (int, error) {
	w.prepare()
	n, err := w.ResponseWriter.Write(p)
	if err != nil {
		w.cancel()
	}
	return n, err
}

type flushingConnectionWriter struct{ *connectionWriter }

func (w *flushingConnectionWriter) Flush() {
	w.prepare()
	if err := w.controller.Flush(); err != nil {
		w.cancel()
	}
}
func boundResponseConnection(w http.ResponseWriter, cancel context.CancelFunc, deadline time.Duration) (http.ResponseWriter, func()) {
	writer := &connectionWriter{ResponseWriter: w, controller: http.NewResponseController(w), cancel: cancel, deadline: deadline}
	cleanup := func() {
		_ = writer.controller.SetWriteDeadline(time.Time{})
		_ = writer.controller.SetReadDeadline(time.Time{})
	}
	if _, ok := w.(http.Flusher); ok {
		return &flushingConnectionWriter{writer}, cleanup
	}
	return writer, cleanup
}
