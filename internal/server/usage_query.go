package server

import (
	"codearts2api/internal/store"
	"net/http"
	"strconv"
)

func (s *Server) handleAdminUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	q := store.UsageQuery{Page: 1, PageSize: 20, Provider: r.URL.Query().Get("provider")}
	for name, target := range map[string]*int64{"start": &q.Start, "end": &q.End} {
		if v := r.URL.Query().Get(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				writeErr(w, 400, "时间参数必须为 Unix 秒", "invalid_request_error")
				return
			}
			*target = n
		}
	}
	if q.Start != 0 && q.End != 0 && q.Start >= q.End {
		writeErr(w, 400, "开始时间必须早于结束时间", "invalid_request_error")
		return
	}
	for name, target := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if v := r.URL.Query().Get(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || (name == "page_size" && n > 100) {
				writeErr(w, 400, "分页参数无效，page_size 范围为 1–100", "invalid_request_error")
				return
			}
			*target = n
		}
	}
	writeJSON(w, 200, s.Store.QueryUsage(q))
}
