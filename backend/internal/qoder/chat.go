package qoder

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"codearts2api/internal/provider"
)

func chatBody(a Credentials, request provider.Request) ([]byte, string, error) {
	model := request.Model
	if model == "" || strings.ContainsAny(model, "/\r\n\x00") {
		return nil, "", &provider.Error{Status: 400, Message: "Qoder 型号与账号地区不匹配"}
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(request.Body, &input) != nil {
		return nil, "", &provider.Error{Status: 400, Message: "Qoder 对话请求必须为 JSON 对象"}
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(input["messages"], &messages) != nil || len(messages) == 0 {
		return nil, "", &provider.Error{Status: 400, Message: "Qoder 请求缺少 messages"}
	}
	id, err := uuid()
	if err != nil {
		return nil, "", err
	}
	session := request.SessionID
	if session == "" {
		session = id
	}
	prompt := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if stringField(messages[i], "role") == "user" {
			_ = json.Unmarshal(messages[i]["content"], &prompt)
			break
		}
	}
	parameters := map[string]json.RawMessage{}
	for _, key := range []string{"max_tokens", "temperature", "top_p", "stop", "seed", "reasoning_effort", "response_format", "tool_choice", "parallel_tool_calls", "thinking"} {
		if value, ok := input[key]; ok {
			parameters[key] = value
		}
	}
	if _, ok := parameters["max_tokens"]; !ok {
		if v, exists := input["max_completion_tokens"]; exists {
			parameters["max_tokens"] = v
		}
	}
	tools := json.RawMessage(`[]`)
	if b, ok := input["tools"]; ok && string(b) != "null" {
		tools = b
	}
	mc := map[string]any{"key": model, "format": "openai", "source": "system"}
	body := map[string]any{
		"request_id": id, "request_set_id": id, "chat_record_id": session, "session_id": session,
		"stream": true, "chat_task": "FREE_INPUT", "is_reply": true, "is_retry": false,
		"session_type": "qodercli", "agent_id": "agent_common", "task_id": "common", "source": 1, "version": "3",
		"aliyun_user_type": a.UserType, "model_config": mc,
		"messages": input["messages"], "tools": tools, "parameters": parameters,
		"chat_context": map[string]any{"text": map[string]string{"type": "text", "text": prompt}, "extra": map[string]any{"originalContent": map[string]string{"type": "text", "text": prompt}, "modelConfig": mc}},
		"business":     map[string]any{"product": "cli", "version": Version, "type": "agent", "id": session, "name": "API chat", "begin_at": time.Now().UnixMilli(), "stage": "start", "model_key": model},
	}
	b, err := json.Marshal(body)
	return b, model, err
}

func (c *Client) Stream(ctx context.Context, raw json.RawMessage, request provider.Request, emit func([]byte) error) error {
	a, err := decode(raw)
	if err != nil {
		return err
	}
	body, model, err := chatBody(a, request)
	if err != nil {
		return err
	}
	resp, err := c.signedRequest(ctx, a, http.MethodPost, ChatPath, model, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return failure(resp.StatusCode)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return &provider.Error{Status: 502, Message: "Qoder 未返回流式对话响应"}
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 65536), 4<<20)
	event := ""
	data := []string{}
	dataSize, finished, observed := 0, false, false
	flush := func() error {
		defer func() { event = ""; data = data[:0]; dataSize = 0 }()
		if event == "finish" {
			if !observed {
				return failure(502)
			}
			finished = true
			return nil
		}
		if len(data) == 0 {
			return nil
		}
		if event == "error" {
			return failure(502)
		}
		var envelope struct {
			Status int             `json:"statusCodeValue"`
			Body   json.RawMessage `json:"body"`
		}
		if json.Unmarshal([]byte(strings.Join(data, "\n")), &envelope) != nil || envelope.Status != 200 {
			return failure(envelope.Status)
		}
		inner := envelope.Body
		var str string
		if json.Unmarshal(inner, &str) == nil {
			inner = []byte(str)
		}
		if string(inner) == "[DONE]" {
			return nil // The separate finish event is authoritative.
		}
		var chunk map[string]json.RawMessage
		if json.Unmarshal(inner, &chunk) != nil || chunk == nil || len(chunk["error"]) > 0 {
			return failure(502)
		}
		var choices []json.RawMessage
		if json.Unmarshal(chunk["choices"], &choices) != nil {
			return failure(502)
		}
		if len(choices) == 0 && (len(chunk["usage"]) == 0 || string(chunk["usage"]) == "null") {
			return failure(502)
		}
		chunk["model"], _ = json.Marshal(provider.ModelID(provider.Qoder, a.Scope, request.Model))
		b, err := json.Marshal(chunk) // RawMessage retains tool arguments and integers.
		if err != nil {
			return failure(502)
		}
		observed = observed || len(choices) > 0
		return emit(b)
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			if finished {
				return nil
			}
		} else if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			part := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			dataSize += len(part) + 1
			if dataSize > 4<<20 {
				return failure(502)
			}
			data = append(data, part)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanner.Err() != nil {
		return &provider.Error{Status: 502, Message: "Qoder 流式响应读取失败"}
	}
	if len(data) > 0 || event != "" {
		if err := flush(); err != nil {
			return err
		}
	}
	if !finished {
		return &provider.Error{Status: 502, Message: "Qoder 响应提前结束，未收到完成事件"}
	}
	return nil
}

var _ provider.Adapter = (*Client)(nil)
var _ provider.BrowserLogin = (*Client)(nil)
var _ provider.LoginReleaser = (*Client)(nil)
