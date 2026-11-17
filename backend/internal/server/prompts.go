package server

import (
	"bytes"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/requestpolicy"
	"encoding/json"
	"net/http"
)

func (s *Server) handlePrompts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, s.Store.Settings().PromptConfiguration())
	case http.MethodPost:
		var body struct {
			Expected      config.PromptConfiguration `json:"expected"`
			Configuration config.PromptConfiguration `json:"configuration"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
		d.DisallowUnknownFields()
		if err := d.Decode(&body); err != nil {
			writeErr(w, 400, "提示词配置格式无效", "invalid_request_error")
			return
		}
		if err := s.Store.SavePromptConfiguration(body.Expected, body.Configuration); err != nil {
			writeModelError(w, err, 400)
			return
		}
		writeJSON(w, 200, s.Store.Settings().PromptConfiguration())
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErr(w, 405, "仅支持 GET / POST", "invalid_request_error")
	}
}
func (s *Server) handlePromptPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		Protocol    provider.Protocol `json:"protocol"`
		Mode        string            `json:"mode"`
		Text        string            `json:"text"`
		RequestJSON string            `json:"request_json"`
		Request     json.RawMessage   `json:"request"`
		Sanitize    bool              `json:"sanitize"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil {
		writeErr(w, 400, "预览参数无效", "invalid_request_error")
		return
	}
	if body.Protocol != provider.ChatProtocol && body.Protocol != provider.ResponsesProtocol && body.Protocol != provider.MessagesProtocol {
		writeErr(w, 400, "不支持的协议", "invalid_request_error")
		return
	}
	if err := (config.PromptSettings{Mode: body.Mode, Text: body.Text}).Validate(); err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	if body.RequestJSON != "" {
		body.Request = json.RawMessage(body.RequestJSON)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body.Request, &object) != nil || object == nil {
		writeErr(w, 400, "请求示例必须为 JSON 对象", "invalid_request_error")
		return
	}
	output, err := requestpolicy.PrepareProtocolBody(body.Request, body.Protocol, body.Mode, body.Text, body.Sanitize)
	if err != nil || !json.Valid(output) {
		writeErr(w, 400, "请求示例不是有效的协议请求", "invalid_request_error")
		return
	}
	var formatted bytes.Buffer
	_ = json.Indent(&formatted, output, "", "  ")
	writeJSON(w, 200, map[string]any{"request": json.RawMessage(output), "request_json": formatted.String()})
}
