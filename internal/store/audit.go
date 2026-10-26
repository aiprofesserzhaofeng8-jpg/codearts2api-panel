package store

import "time"

type AuditEvent struct {
	Time      int64  `json:"time"`
	Actor     string `json:"actor"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Peer      string `json:"peer"`
	Status    int    `json:"status"`
}

func (s *Store) Audit(e AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Time == 0 {
		e.Time = time.Now().Unix()
	}
	s.st.Audit = append(s.st.Audit, e)
	if len(s.st.Audit) > 5000 {
		s.st.Audit = append([]AuditEvent(nil), s.st.Audit[len(s.st.Audit)-5000:]...)
	}
}
func (s *Store) AuditEvents() []AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]AuditEvent{}, s.st.Audit...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
