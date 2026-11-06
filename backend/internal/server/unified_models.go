package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"codearts2api/internal/apierr"
	"codearts2api/internal/config"
)

func writeModelError(w http.ResponseWriter, err error, fallback int) {
	var model *config.ModelError
	if errors.As(err, &model) {
		e := apierr.From(err)
		writeErr(w, e.Status, err.Error(), e.OpenAI, model.Reason)
		return
	}
	writeErr(w, fallback, err.Error(), "invalid_request_error", "invalid_model_definition")
}

// Strict object decoder shared by model-related management writes. Required
// fields are checked separately so missing booleans/lists never grant access.
func decodeModelObject(w http.ResponseWriter, r *http.Request, limit int64, target any, required ...string) (map[string]json.RawMessage, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			writeErr(w, 413, "请求体过大", "invalid_request_error")
			return nil, false
		}
		writeErr(w, 400, "请求体读取失败", "invalid_request_error", "invalid_model_definition")
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		writeErr(w, 400, "请求须为单个 JSON 对象", "invalid_request_error", "invalid_model_definition")
		return nil, false
	}
	for _, key := range required {
		v, ok := fields[key]
		if !ok || (key != "client_policy" && bytes.Equal(bytes.TrimSpace(v), []byte("null"))) {
			writeErr(w, 400, "缺少字段或字段为空: "+key, "invalid_request_error", "invalid_model_definition")
			return nil, false
		}
	}
	for key, value := range fields {
		if key == "expected_revision" || key == "expected_routing_revision" {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				writeErr(w, 400, "版本条件不能为 null", "invalid_request_error", "invalid_model_definition")
				return nil, false
			}
		}
		if (key == "client_policy" || key == "policy") && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			var policy map[string]json.RawMessage
			if json.Unmarshal(value, &policy) != nil {
				writeErr(w, 400, "策略格式无效", "invalid_request_error", "invalid_model_definition")
				return nil, false
			}
			for _, field := range []string{"context_window", "max_output_tokens", "reserve_tokens", "keep_recent_tokens", "trigger_tokens", "summary_max_tokens", "summary_enabled", "summary_retry", "disable_summary_thinking", "source"} {
				if _, present := policy[field]; !present {
					writeErr(w, 400, "须提供完整客户端策略", "invalid_request_error", "invalid_model_definition")
					return nil, false
				}
			}
			for _, field := range policy {
				if bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
					writeErr(w, 400, "策略字段不能为 null", "invalid_request_error", "invalid_model_definition")
					return nil, false
				}
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		writeErr(w, 400, "配置包含未知字段或格式无效", "invalid_request_error", "invalid_model_definition")
		return nil, false
	}
	return fields, true
}

func (s *Server) handleUnifiedModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := strings.TrimPrefix(r.URL.Path, "/admin/api/unified-models")
	if id != "" {
		if !strings.HasPrefix(id, "/") || !config.ValidUnifiedID(strings.TrimPrefix(id, "/")) {
			writeErr(w, 400, "统一模型标识无效", "invalid_request_error", "invalid_model_definition")
			return
		}
		id = strings.TrimPrefix(id, "/")
	}
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			detail, err := s.UnifiedModels.Detail(id)
			if err != nil {
				writeModelError(w, err, 400)
				return
			}
			writeJSON(w, 200, detail)
			return
		}
		limit, after := 100, r.URL.Query().Get("after")
		if values, present := r.URL.Query()["limit"]; present {
			var err error
			if len(values) != 1 {
				writeErr(w, 400, "分页参数无效", "invalid_request_error")
				return
			}
			limit, err = strconv.Atoi(values[0])
			if err != nil || limit < 1 || limit > 200 {
				writeErr(w, 400, "分页参数无效", "invalid_request_error")
				return
			}
		}
		if (after != "" && !config.ValidUnifiedID(after)) || len(r.URL.Query()["after"]) > 1 {
			writeErr(w, 400, "分页游标无效", "invalid_request_error")
			return
		}
		snapshot := s.UnifiedModels.Snapshot()
		sort.Slice(snapshot.Models, func(i, j int) bool { return snapshot.Models[i].ID < snapshot.Models[j].ID })
		models, next := []config.UnifiedModel{}, ""
		for _, model := range snapshot.Models {
			if model.ID > after {
				if len(models) == limit {
					next = models[len(models)-1].ID
					break
				}
				models = append(models, model)
			}
		}
		writeJSON(w, 200, map[string]any{"models": models, "next_after": next, "routing_revision": snapshot.Revision, "compatibility_aliases": s.UnifiedModels.CompatibilityAliases(snapshot)})
	case http.MethodPost, http.MethodPut:
		if (r.Method == http.MethodPost && id != "") || (r.Method == http.MethodPut && id == "") {
			writeErr(w, 405, "此路径不支持该方法", "invalid_request_error")
			return
		}
		var body struct {
			Name                    string               `json:"name"`
			Enabled                 bool                 `json:"enabled"`
			Sources                 []config.ModelSource `json:"sources"`
			ClientPolicy            *config.ModelPolicy  `json:"client_policy"`
			ExpectedRevision        uint64               `json:"expected_revision"`
			ExpectedRoutingRevision uint64               `json:"expected_routing_revision"`
		}
		fields, ok := decodeModelObject(w, r, 32<<10, &body, "name", "enabled", "sources", "client_policy")
		if !ok {
			return
		}
		if id == "" {
			if _, ok := fields["expected_revision"]; ok {
				writeErr(w, 400, "创建时不能指定模型旧版本", "invalid_request_error", "invalid_model_definition")
				return
			}
		}
		model, err := s.UnifiedModels.Save(config.UnifiedModel{ID: id, Name: body.Name, Enabled: body.Enabled, Sources: body.Sources}, body.ClientPolicy, body.ExpectedRevision, body.ExpectedRoutingRevision)
		if err != nil {
			writeModelError(w, err, 400)
			return
		}
		detail, err := s.UnifiedModels.Detail(model.ID)
		if err != nil {
			writeModelError(w, err, 500)
			return
		}
		status := 200
		if id == "" {
			status = 201
		}
		writeJSON(w, status, detail)
	case http.MethodDelete:
		if id == "" {
			writeErr(w, 405, "删除须指定模型", "invalid_request_error")
			return
		}
		var body struct {
			ExpectedRevision        uint64 `json:"expected_revision"`
			ExpectedRoutingRevision uint64 `json:"expected_routing_revision"`
		}
		if _, ok := decodeModelObject(w, r, 32<<10, &body); !ok {
			return
		}
		if err := s.UnifiedModels.Delete(id, body.ExpectedRevision, body.ExpectedRoutingRevision); err != nil {
			writeModelError(w, err, 400)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "routing_revision": s.UnifiedModels.Snapshot().Revision})
	default:
		writeErr(w, 405, "不支持的方法", "invalid_request_error")
	}
}
