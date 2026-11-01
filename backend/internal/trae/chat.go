package trae

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"codearts2api/internal/provider"
)

func invalid(message string) error {
	return &provider.Error{Status: 400, Message: "Trae 请求无效：" + message}
}

// Translate the native structured protocol, keeping tool execution with the caller.
func prepareBody(raw []byte, model string) (map[string]any, error) {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, invalid("需要 JSON 对象")
	}
	if n, ok := obj["n"].(float64); ok && n != 1 {
		return nil, invalid("当前仅支持 n=1")
	}
	if _, ok := obj["functions"]; ok {
		return nil, invalid("请使用 tools 字段代替 legacy functions")
	}
	msgs, ok := obj["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return nil, invalid("messages 不能为空")
	}
	for _, item := range msgs {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, invalid("消息格式无效")
		}
		role, _ := m["role"].(string)
		if role == "developer" {
			m["role"] = "system"
		}
		if role == "assistant" {
			if calls, exists := m["tool_calls"]; exists {
				list, ok := calls.([]any)
				if !ok {
					return nil, invalid("tool_calls 必须为数组")
				}
				for _, call := range list {
					tc, ok := call.(map[string]any)
					if !ok {
						return nil, invalid("工具历史无效")
					}
					fn, ok := tc["function"].(map[string]any)
					if !ok {
						return nil, invalid("工具历史缺少 function")
					}
					if name, ok := fn["name"].(string); !ok || name == "" {
						return nil, invalid("工具历史缺少名称")
					}
					tc["function_call"] = fn
					delete(tc, "function")
				}
			}
		}
		switch v := m["content"].(type) {
		case string:
			m["content"] = []any{map[string]any{"type": "text", "text": v}}
		case []any:
			for _, part := range v {
				b, ok := part.(map[string]any)
				if !ok || b["type"] != "text" {
					return nil, invalid("当前 Trae 适配仅支持文本消息，暂不支持图片或音频")
				}
				if _, ok := b["text"].(string); !ok {
					return nil, invalid("text 内容必须为字符串")
				}
			}
		case nil:
		default:
			return nil, invalid("content 类型无效")
		}
	}
	obj["model"] = model
	obj["config_name"] = model
	obj["function"] = "solo_work_lite"
	obj["stream"] = true
	if alias, exists := obj["max_completion_tokens"]; exists {
		if current, ok := obj["max_tokens"]; ok {
			a, aok := alias.(float64)
			b, bok := current.(float64)
			if !aok || !bok {
				return nil, invalid("输出预算必须为数字")
			}
			obj["max_tokens"] = min(a, b)
		} else {
			obj["max_tokens"] = alias
		}
		delete(obj, "max_completion_tokens")
	}
	if limit, exists := obj["max_tokens"]; exists {
		n, ok := limit.(float64)
		if !ok || n < 1 || n != float64(int64(n)) {
			return nil, invalid("输出预算必须为正整数")
		}
	}
	if choice, exists := obj["tool_choice"]; exists {
		switch v := choice.(type) {
		case string:
			if v != "auto" && v != "required" && v != "none" {
				return nil, invalid("tool_choice 无效")
			}
			if v == "none" {
				delete(obj, "tools")
				delete(obj, "tool_choice")
			}
		case map[string]any:
			typ, _ := v["type"].(string)
			if typ != "function" {
				return nil, invalid("tool_choice 对象须指定函数")
			}
			fn, ok := v["function"].(map[string]any)
			if !ok {
				return nil, invalid("指定函数缺少定义")
			}
			name, ok := fn["name"].(string)
			if !ok || name == "" {
				return nil, invalid("指定函数缺少名称")
			}
			obj["tool_choice"] = name
		default:
			return nil, invalid("tool_choice 类型无效")
		}
	}
	if rawTools, exists := obj["tools"]; exists {
		tools, ok := rawTools.([]any)
		if !ok {
			return nil, invalid("tools 必须为数组")
		}
		for _, tool := range tools {
			t, ok := tool.(map[string]any)
			if !ok || t["type"] != "function" {
				return nil, invalid("仅支持调用方 function 工具")
			}
			fn, ok := t["function"].(map[string]any)
			if !ok {
				return nil, invalid("缺少 function")
			}
			if name, ok := fn["name"].(string); !ok || name == "" {
				return nil, invalid("函数名不能为空")
			}
			params, exists := fn["parameters"]
			if !exists {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			if _, ok := params.(map[string]any); !ok {
				return nil, invalid("parameters 必须为 JSON 对象")
			}
			encoded, _ := json.Marshal(params)
			fn["parameters"] = string(encoded)
		}
	}
	return obj, nil
}

type toolState struct {
	ID, Name, Arguments string
	Index               int
}
type streamState struct {
	id, model            string
	created              int64
	seen, done, hasTools bool
	usage                json.RawMessage
	tools                map[int]*toolState
	byID                 map[string]int
	last                 int
	bytes                int
}

func newStream(model string) *streamState {
	return &streamState{id: "chatcmpl-" + randomID(), model: model, created: time.Now().Unix(), tools: map[int]*toolState{}, byID: map[string]int{}, last: -1}
}
func (s *streamState) chunk(delta any, finish any, usage json.RawMessage) []byte {
	choices := []any{}
	if delta != nil || finish != nil {
		choices = append(choices, map[string]any{"index": 0, "delta": delta, "finish_reason": finish})
	}
	m := map[string]any{"id": s.id, "object": "chat.completion.chunk", "created": s.created, "model": s.model, "choices": choices}
	if len(usage) > 0 {
		m["usage"] = usage
	}
	raw, _ := json.Marshal(m)
	return raw
}
func normalizeUsage(raw []byte) (json.RawMessage, error) {
	var u map[string]json.RawMessage
	if json.Unmarshal(raw, &u) != nil || u == nil {
		return nil, fmt.Errorf("Trae 用量事件格式无效")
	}
	for _, key := range []string{"prompt_tokens", "completion_tokens"} {
		var n int64
		if json.Unmarshal(u[key], &n) != nil || n < 0 {
			return nil, nil
		}
	}
	for _, pair := range []struct{ top, nested, key string }{{"reasoning_tokens", "completion_tokens_details", "reasoning_tokens"}, {"cache_read_input_tokens", "prompt_tokens_details", "cached_tokens"}, {"cached_tokens", "prompt_tokens_details", "cached_tokens"}, {"prompt_cache_hit_tokens", "prompt_tokens_details", "cached_tokens"}} {
		var n int64
		if json.Unmarshal(u[pair.top], &n) == nil && n > 0 {
			var d map[string]json.RawMessage
			_ = json.Unmarshal(u[pair.nested], &d)
			if d == nil {
				d = map[string]json.RawMessage{}
			}
			var existing int64
			_ = json.Unmarshal(d[pair.key], &existing)
			d[pair.key], _ = json.Marshal(max(existing, n))
			u[pair.nested], _ = json.Marshal(d)
		}
	}
	out, _ := json.Marshal(u)
	return out, nil
}
func (s *streamState) toolDeltas(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var list []map[string]json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		var one map[string]json.RawMessage
		if json.Unmarshal(raw, &one) != nil || one == nil {
			return nil, fmt.Errorf("Trae 工具事件格式无效")
		}
		list = []map[string]json.RawMessage{one}
	}
	out := []any{}
	for _, call := range list {
		index := -1
		id := text(call, "id")
		if b, ok := call["index"]; ok {
			if json.Unmarshal(b, &index) != nil || index < 0 || index > 1023 {
				return nil, fmt.Errorf("Trae 工具序号无效")
			}
		} else if known, ok := s.byID[id]; id != "" && ok {
			index = known
		} else if id != "" {
			index = len(s.tools)
			for s.tools[index] != nil {
				index++
			}
		} else if s.last >= 0 {
			index = s.last
		} else {
			index = 0
		}
		st := s.tools[index]
		fresh := st == nil
		if fresh {
			st = &toolState{Index: index, ID: id}
			if st.ID == "" {
				st.ID = "call_" + randomID()
			}
			s.tools[index] = st
		}
		if id != "" {
			if known, ok := s.byID[id]; ok && known != index {
				return nil, fmt.Errorf("Trae 工具标识冲突")
			}
			s.byID[id] = index
			if !fresh && id != st.ID {
				return nil, fmt.Errorf("Trae 工具标识变化")
			}
		}
		s.last = index
		var fn map[string]json.RawMessage
		b := call["function_call"]
		if len(b) == 0 {
			b = call["function"]
		}
		if json.Unmarshal(b, &fn) != nil || fn == nil {
			return nil, fmt.Errorf("Trae 工具事件缺少函数信息")
		}
		deltaFn := map[string]any{}
		name := text(fn, "name")
		if name != "" {
			if st.Name != "" && st.Name != name {
				return nil, fmt.Errorf("Trae 工具名称变化")
			}
			if st.Name == "" {
				st.Name = name
				deltaFn["name"] = name
			}
		}
		if b, ok := fn["arguments"]; ok && string(b) != "null" {
			var arg string
			if json.Unmarshal(b, &arg) != nil {
				if !json.Valid(b) {
					return nil, fmt.Errorf("Trae 工具参数无效")
				}
				arg = string(b)
			}
			st.Arguments += arg
			if len(st.Arguments) > 1<<20 {
				return nil, fmt.Errorf("Trae 工具参数超过上限")
			}
			deltaFn["arguments"] = arg
		}
		delta := map[string]any{"index": index, "function": deltaFn}
		if fresh {
			delta["id"] = st.ID
			delta["type"] = "function"
		}
		out = append(out, delta)
	}
	return out, nil
}
func (s *streamState) event(name string, raw []byte, emit func([]byte) error) error {
	if s.done {
		return nil
	}
	s.bytes += len(raw)
	if s.bytes > 32<<20 {
		return fmt.Errorf("Trae 响应超过 32 MiB")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("[DONE]")) {
		return fmt.Errorf("Trae 缺少带有结束信息的 done 事件")
	}
	if err := errorFrom(200, raw, name == "error"); err != nil {
		return err
	}
	if !json.Valid(raw) {
		return fmt.Errorf("Trae SSE 事件不是有效 JSON")
	}
	switch name {
	case "metadata", "timing_cost", "extra_info", "ping", "heartbeat":
		return nil
	case "output":
		var e struct {
			Response  string          `json:"response"`
			Reasoning string          `json:"reasoning_content"`
			Calls     json.RawMessage `json:"tool_calls"`
		}
		if json.Unmarshal(raw, &e) != nil {
			return fmt.Errorf("Trae 输出事件格式无效")
		}
		delta := map[string]any{}
		if e.Response != "" {
			delta["content"] = e.Response
		}
		if e.Reasoning != "" {
			delta["reasoning_content"] = e.Reasoning
		}
		calls, err := s.toolDeltas(e.Calls)
		if err != nil {
			return err
		}
		if len(calls) > 0 {
			delta["tool_calls"] = calls
			s.hasTools = true
		}
		if len(delta) == 0 {
			return nil
		}
		if !s.seen {
			delta["role"] = "assistant"
		}
		s.seen = true
		return emit(s.chunk(delta, nil, nil))
	case "token_usage":
		u, err := normalizeUsage(raw)
		if err != nil {
			return err
		}
		if len(u) > 0 {
			s.usage = u
			return emit(s.chunk(nil, nil, u))
		}
		return nil
	case "done":
		var e struct {
			Finish string `json:"finish_reason"`
		}
		if json.Unmarshal(raw, &e) != nil {
			return fmt.Errorf("Trae 结束事件无效")
		}
		if !s.seen {
			return fmt.Errorf("Trae 在返回正文或工具调用前结束")
		}
		finish := e.Finish
		if finish == "" {
			finish = "stop"
		}
		if finish == "stop" && s.hasTools {
			finish = "tool_calls"
		}
		switch finish {
		case "stop", "length", "tool_calls", "content_filter":
		default:
			return fmt.Errorf("Trae 返回未知结束原因")
		}
		if finish == "tool_calls" {
			if !s.hasTools {
				return fmt.Errorf("Trae 工具结束事件缺少工具调用")
			}
			for _, tc := range s.tools {
				if tc.Name == "" || !json.Valid([]byte(tc.Arguments)) {
					return fmt.Errorf("Trae 工具参数不完整，未返回成功完成")
				}
			}
		}
		s.done = true
		return emit(s.chunk(map[string]any{}, finish, s.usage))
	default:
		return nil
	}
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, input provider.Request, onChunk func([]byte) error) error {
	a, err := decode(raw)
	if err != nil {
		return err
	}
	body, err := prepareBody(input.Body, input.Model)
	if err != nil {
		return err
	}
	resp, err := c.request(ctx, a, "stream", c.AgentBase+ChatPath, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
		return errorFrom(resp.StatusCode, b, true)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
		if err := errorFrom(resp.StatusCode, b, false); err != nil {
			return err
		}
		return fmt.Errorf("Trae 未返回事件流")
	}
	st := newStream(provider.ModelID(provider.Trae, "cn", input.Model))
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 65536), 2<<20)
	event := ""
	parts := []string{}
	size := 0
	emit := func() error {
		if len(parts) == 0 {
			event = ""
			size = 0
			return nil
		}
		err := st.event(event, []byte(strings.Join(parts, "\n")), onChunk)
		event = ""
		parts = nil
		size = 0
		return err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := emit(); err != nil {
				return err
			}
			if st.done {
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			part := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			size += len(part)
			if size > 2<<20 {
				return fmt.Errorf("Trae 单个事件超过 2 MiB")
			}
			parts = append(parts, part)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("Trae 事件流读取失败")
	}
	if err := emit(); err != nil {
		return err
	}
	if !st.done {
		return fmt.Errorf("Trae 响应在完成前中断，缺少 done 事件")
	}
	return nil
}
