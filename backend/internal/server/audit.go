package server

import (
	"bytes"
	"codearts2api/internal/storage"
	"encoding/json"
	"io"
	"net"
	"net/http"
)

func (s *Server) audit(r *http.Request, actor, operation string, status int) {
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.Store.Audit(storage.AuditEvent{Actor: actor, Operation: operation, Path: r.URL.Path, Peer: peer, Status: status})
}

type auditWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(b)
}
func (s *Server) serveAudited(w http.ResponseWriter, r *http.Request, actor string, next http.HandlerFunc) {
	if r.Method == "GET" || r.Method == "HEAD" {
		next(w, r)
		return
	}
	operation := r.Method
	if r.Body != nil {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			writeErr(w, 413, "管理请求过大", "invalid_request_error")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var action struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(raw, &action)
		switch action.Action {
		case "start", "check", "complete", "cancel", "export", "remove", "pause", "resume", "manual", "session-json", "import", "toggle", "clear-errors", "reimport", "probe":
			operation = action.Action
		}
	}
	recorder := &auditWriter{ResponseWriter: w}
	next(recorder, r)
	status := recorder.status
	if status == 0 {
		status = 200
	}
	s.audit(r, actor, operation, status)
}
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeErr(w, 405, "审计记录只读", "invalid_request_error")
		return
	}
	events := s.Store.AuditEvents()
	size := intQuery(r, "page_size")
	if size <= 0 {
		size = 50
	}
	size = min(size, 200)
	page := intQuery(r, "page")
	if page <= 0 {
		page = 1
	}
	page = min(page, max(1, (len(events)+size-1)/size))
	start := min((page-1)*size, len(events))
	end := min(start+size, len(events))
	writeJSON(w, 200, map[string]any{"events": events[start:end], "total": len(events), "page": page, "page_size": size})
}
