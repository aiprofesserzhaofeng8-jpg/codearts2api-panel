package server

import (
	"codearts2api/internal/apierr"
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const searchServicesPath = "/admin/api/search-services"

func writeSearchError(w http.ResponseWriter, err error) {
	var upstream *provider.Error
	if errors.As(err, &upstream) {
		code := upstream.Code
		if code == "" {
			code = "search_upstream_error"
		}
		writeErr(w, upstream.Status, upstream.Message, apierr.From(err).OpenAI, code)
		return
	}
	writeAPIErr(w, errOpenAI, err)
}

func (s *Server) runSearchService(ctx context.Context, v storage.SearchService, input provider.SearchRequest) (*provider.SearchOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return s.Search.External(ctx, v.Provider, v.Endpoint, v.APIKey, input)
}
func (s *Server) handleSearchServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, searchServicesPath)
	id, action, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	var err error
	switch {
	case r.Method == "GET" && path == "":
		catalog := s.Store.SearchCatalog()
		services := make([]map[string]any, 0, len(catalog.Services))
		for _, v := range catalog.Services {
			services = append(services, map[string]any{"id": v.ID, "name": v.Name, "provider": v.Provider, "endpoint": v.Endpoint, "enabled": v.Enabled, "api_key_set": v.APIKey != ""})
		}
		writeJSON(w, 200, map[string]any{"services": services, "revision": catalog.Revision, "default_service_id": catalog.DefaultID})
		return
	case r.Method == "PUT" && path == "/default":
		var body struct {
			ID       string `json:"service_id"`
			Revision uint64 `json:"expected_revision"`
		}
		if _, ok := decodeModelObject(w, r, 8192, &body, "service_id", "expected_revision"); !ok {
			return
		}
		err = s.Store.SetDefaultSearchService(body.ID, body.Revision)
	case (r.Method == "POST" && path == "") || (r.Method == "PATCH" && id != "" && action == ""):
		var body struct {
			storage.SearchServicePatch
			Revision uint64 `json:"expected_revision"`
		}
		if _, ok := decodeModelObject(w, r, 16384, &body, "expected_revision"); !ok {
			return
		}
		id, err = s.Store.SaveSearchService(id, body.SearchServicePatch, body.Revision)
	case r.Method == "DELETE" && id != "" && action == "":
		revision, e := strconv.ParseUint(r.URL.Query().Get("expected_revision"), 10, 64)
		if e != nil {
			writeErr(w, 400, "需要有效的配置版本", "invalid_request_error")
			return
		}
		err = s.Store.DeleteSearchService(id, revision)
	case r.Method == "POST" && id != "" && action == "test":
		for _, v := range s.Store.SearchCatalog().Services {
			if v.ID != id {
				continue
			}
			start := time.Now()
			out, e := s.runSearchService(r.Context(), v, provider.SearchRequest{Query: "DeepSeek", MaxResults: 2})
			if e != nil {
				writeSearchError(w, e)
				return
			}
			writeJSON(w, 200, map[string]any{"latency_ms": time.Since(start).Milliseconds(), "result_count": len(out.Results), "results": out.Results})
			return
		}
		writeErr(w, 404, "搜索服务不存在", "invalid_request_error")
		return
	default:
		writeErr(w, 405, "不支持该操作", "invalid_request_error")
		return
	}
	if err != nil {
		writeModelError(w, err, 400)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id})
}

func (s *Server) handleExternalSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Query      string `json:"query"`
		MaxResults *int   `json:"max_results"`
		ServiceID  string `json:"service_id"`
	}
	if _, ok := decodeModelObject(w, r, 16384, &body, "query"); !ok {
		return
	}
	count := 5
	if body.MaxResults != nil {
		count = *body.MaxResults
	}
	if strings.TrimSpace(body.Query) == "" || count < 1 || count > 10 {
		writeErr(w, 400, "请输入搜索词，结果数须为 1–10", "invalid_request_error")
		return
	}
	key := groupKey(r.Context())
	v, release, err := s.Store.BeginExternalSearch(key, body.ServiceID)
	if err != nil {
		writeSearchError(w, err)
		return
	}
	defer release()
	if v.Provider == "deepseek" && utf8.RuneCountInString(body.Query) > 128 {
		writeErr(w, 400, "DeepSeek 搜索词不能超过 128 个字符", "invalid_request_error")
		return
	}
	start := time.Now()
	id := codearts.RandomHex(16)
	out, err := s.runSearchService(r.Context(), v, provider.SearchRequest{Query: body.Query, MaxResults: count})
	userID, _ := r.Context().Value(inferenceUserKey{}).(string)
	entry := storage.LogEntry{RequestID: id, RequestKind: "search", Protocol: "web_search", SearchServiceID: v.ID, Provider: v.Provider, UserID: userID, Time: start.Unix(), LatencyMS: time.Since(start).Milliseconds(), Attempts: 1, Status: 200}
	if err != nil {
		entry.Status = apierr.From(err).Status
		entry.Err = err.Error()
	}
	if saveErr := s.Store.Record(entry, 0, key); saveErr != nil {
		writeAPIErr(w, errOpenAI, storage.ErrUsagePersistence)
		return
	}
	if err != nil {
		writeSearchError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"request_id": id, "service_id": v.ID, "provider": v.Provider, "results": out.Results})
}
