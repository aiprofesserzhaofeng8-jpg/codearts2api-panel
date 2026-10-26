package server

import (
	"codearts2api/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminUsageRejectsInvalidFilters(t *testing.T) {
	s := newTestServer(t, 0, 1)
	for _, query := range []string{"start=-1", "start=no", "start=20&end=10", "page=0", "page_size=101", "page_size=0", "page=9999999999999999999999"} {
		w := httptest.NewRecorder()
		s.handleAdminUsage(w, httptest.NewRequest("GET", "/admin/api/usage?"+query, nil))
		if w.Code != 400 {
			t.Errorf("%s => %d", query, w.Code)
		}
	}
	s.Store.Record(store.LogEntry{Time: 100, Status: 200}, 0)
	w := httptest.NewRecorder()
	s.handleAdminUsage(w, httptest.NewRequest("GET", "/admin/api/usage?start=100&end=101&page=1&page_size=20", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("bad query: %d %s", w.Code, w.Body.String())
	}
}
