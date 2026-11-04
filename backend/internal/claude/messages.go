package claude

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"strings"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

func messagesBody(r provider.Request) (any, error) {
	bad := func(s string) (any, error) { return nil, providerhttp.Invalid("Claude", s) }
	var in map[string]json.RawMessage
	if json.Unmarshal(r.Body, &in) != nil || in == nil {
		return bad("请求格式无效")
	}
	for _, k := range []string{"seed", "frequency_penalty", "presence_penalty", "logprobs", "top_logprobs", "response_format"} {
		if b := in[k]; len(b) > 0 && string(b) != "null" {
			return bad("Messages 转换不支持 " + k)
		}
	}
	if b := in["n"]; len(b) > 0 && string(b) != "1" {
		return bad("仅支持 n=1")
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(in["messages"], &messages) != nil || len(messages) == 0 {
		return bad("缺少 messages")
	}
	var system []string
	var converted []map[string]any
	appendMessage := func(role string, parts []any) {
		if len(parts) == 0 {
			return
		}
		if len(converted) > 0 && converted[len(converted)-1]["role"] == role {
			old := converted[len(converted)-1]["content"].([]any)
			converted[len(converted)-1]["content"] = append(old, parts...)
		} else {
			converted = append(converted, map[string]any{"role": role, "content": parts})
		}
	}
	for _, m := range messages {
		role := providerhttp.Field(m, "role")
		if role == "tool" {
			id := providerhttp.Field(m, "tool_call_id")
			var content string
			if id == "" || json.Unmarshal(m["content"], &content) != nil {
				return bad("工具结果需包含 tool_call_id 和文本 content")
			}
			appendMessage("user", []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content}})
			continue
		}
		if role != "system" && role != "developer" && role != "user" && role != "assistant" {
			return bad("未知消息角色")
		}
		var parts []any
		if b := m["anthropic_thinking_blocks"]; len(b) > 0 && string(b) != "null" {
			var blocks []map[string]json.RawMessage
			if role != "assistant" || json.Unmarshal(b, &blocks) != nil {
				return bad("历史思考块格式无效")
			}
			for _, block := range blocks {
				typ := providerhttp.Field(block, "type")
				if typ == "thinking" {
					if providerhttp.Field(block, "signature") == "" {
						return bad("历史思考缺少 signature")
					}
				} else if typ != "redacted_thinking" || providerhttp.Field(block, "data") == "" {
					return bad("历史思考块无效")
				}
				parts = append(parts, block)
			}
		}
		if text := providerhttp.Field(m, "reasoning_content"); text != "" && len(parts) == 0 {
			signature := providerhttp.Field(m, "reasoning_signature")
			if signature == "" {
				return bad("历史思考缺少 reasoning_signature")
			}
			parts = append(parts, map[string]string{"type": "thinking", "thinking": text, "signature": signature})
		}
		var text string
		var blocks []map[string]json.RawMessage
		if b := m["content"]; len(b) > 0 && string(b) != "null" {
			if json.Unmarshal(b, &text) == nil {
				if text != "" {
					parts = append(parts, map[string]string{"type": "text", "text": text})
				}
			} else if json.Unmarshal(b, &blocks) == nil {
				for _, b := range blocks {
					switch providerhttp.Field(b, "type") {
					case "text":
						parts = append(parts, map[string]string{"type": "text", "text": providerhttp.Field(b, "text")})
					case "image_url":
						if role != "user" {
							return bad("图片只支持用户消息")
						}
						var image map[string]json.RawMessage
						if json.Unmarshal(b["image_url"], &image) != nil {
							return bad("图片无效")
						}
						u := providerhttp.Field(image, "url")
						var source map[string]string
						if strings.HasPrefix(u, "data:") {
							meta, data, ok := strings.Cut(strings.TrimPrefix(u, "data:"), ",")
							media, encoding, ok2 := strings.Cut(meta, ";")
							if !ok || !ok2 || encoding != "base64" || data == "" || (media != "image/png" && media != "image/jpeg" && media != "image/gif" && media != "image/webp") {
								return bad("不支持该图片 data URL")
							}
							source = map[string]string{"type": "base64", "media_type": media, "data": data}
						} else {
							parsed, e := url.Parse(u)
							if e != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
								return bad("图片 URL 须为 HTTPS")
							}
							source = map[string]string{"type": "url", "url": u}
						}
						parts = append(parts, map[string]any{"type": "image", "source": source})
					default:
						return bad("暂不支持该内容块")
					}
				}
			} else {
				return bad("content 格式无效")
			}
		}
		if role == "system" || role == "developer" {
			for _, p := range parts {
				v, ok := p.(map[string]string)
				if !ok || v["type"] != "text" {
					return bad("系统消息仅支持文本")
				}
				system = append(system, v["text"])
			}
			continue
		}
		if refusal := providerhttp.Field(m, "refusal"); refusal != "" {
			if role != "assistant" {
				return bad("refusal 只支持助手消息")
			}
			parts = append(parts, map[string]string{"type": "text", "text": refusal})
		}
		var calls []struct {
			ID, Type string
			Function struct{ Name, Arguments string }
		}
		if b := m["tool_calls"]; len(b) > 0 && string(b) != "null" && json.Unmarshal(b, &calls) != nil {
			return bad("tool_calls 格式无效")
		}
		for _, call := range calls {
			var args map[string]json.RawMessage
			if role != "assistant" || call.ID == "" || call.Type != "function" || call.Function.Name == "" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
				return bad("工具参数必须为完整 JSON 对象")
			}
			parts = append(parts, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": json.RawMessage(call.Function.Arguments)})
		}
		appendMessage(role, parts)
	}
	if len(converted) == 0 || converted[0]["role"] != "user" {
		return bad("Messages 对话必须以用户消息开始")
	}
	out := map[string]any{"model": r.Model, "stream": true, "messages": converted, "max_tokens": 4096}
	if len(system) > 0 {
		out["system"] = strings.Join(system, "\n\n")
	}
	for _, k := range []string{"max_tokens", "temperature", "top_p"} {
		if b := in[k]; len(b) > 0 && string(b) != "null" {
			out[k] = b
		}
	}
	if b := in["max_completion_tokens"]; len(b) > 0 && string(b) != "null" {
		out["max_tokens"] = b
	}
	if b, ok := out["max_tokens"].(json.RawMessage); ok {
		var n int
		if json.Unmarshal(b, &n) != nil || n <= 0 {
			return bad("max_tokens 必须为正整数")
		}
	}
	if b := in["stop"]; len(b) > 0 && string(b) != "null" {
		var s string
		var ss []string
		if json.Unmarshal(b, &s) == nil {
			ss = []string{s}
		} else if json.Unmarshal(b, &ss) != nil {
			return bad("stop 格式无效")
		}
		out["stop_sequences"] = ss
	}
	if b := in["thinking"]; len(b) > 0 && string(b) != "null" {
		var t map[string]json.RawMessage
		if json.Unmarshal(b, &t) != nil {
			return bad("thinking 格式无效")
		}
		switch providerhttp.Field(t, "type") {
		case "enabled":
			var budget int
			if json.Unmarshal(t["budget_tokens"], &budget) != nil || budget < 1024 {
				return bad("thinking.budget_tokens 至少为 1024")
			}
		case "adaptive", "disabled":
			if len(t) != 1 {
				return bad("adaptive / disabled 不接受额外思考字段")
			}
		default:
			return bad("thinking.type 无效")
		}
		out["thinking"] = t
	}
	if b := in["output_config"]; len(b) > 0 && string(b) != "null" {
		var config map[string]json.RawMessage
		if json.Unmarshal(b, &config) != nil || config == nil {
			return bad("output_config 格式无效")
		}
		out["output_config"] = config
	}
	if effort := providerhttp.Field(in, "reasoning_effort"); effort != "" {
		if out["thinking"] != nil || out["output_config"] != nil {
			return bad("不能同时设置 reasoning_effort 和原生思考参数")
		}
		switch effort {
		case "none":
			out["thinking"] = map[string]string{"type": "disabled"}
		case "low", "medium", "high", "max":
			out["thinking"] = map[string]string{"type": "adaptive"}
			out["output_config"] = map[string]string{"effort": effort}
		default:
			return bad("不支持该 reasoning_effort")
		}
	}
	var tools []map[string]json.RawMessage
	if b := in["tools"]; len(b) > 0 && string(b) != "null" && json.Unmarshal(b, &tools) != nil {
		return bad("tools 格式无效")
	}
	if len(tools) > 0 {
		var tt []any
		for _, t := range tools {
			var f map[string]json.RawMessage
			if providerhttp.Field(t, "type") != "function" || json.Unmarshal(t["function"], &f) != nil || providerhttp.Field(f, "name") == "" {
				return bad("仅支持 function 工具")
			}
			converted := map[string]any{"name": f["name"], "input_schema": json.RawMessage(`{"type":"object","properties":{}}`)}
			if len(f["parameters"]) > 0 {
				converted["input_schema"] = f["parameters"]
			}
			if len(f["description"]) > 0 {
				converted["description"] = f["description"]
			}
			tt = append(tt, converted)
		}
		out["tools"] = tt
	}
	var choice map[string]any
	if b := in["tool_choice"]; len(b) > 0 && string(b) != "null" {
		var s string
		if json.Unmarshal(b, &s) == nil {
			switch s {
			case "auto", "none":
				choice = map[string]any{"type": s}
			case "required":
				choice = map[string]any{"type": "any"}
			default:
				return bad("tool_choice 无效")
			}
		} else {
			var t map[string]json.RawMessage
			var f map[string]json.RawMessage
			if json.Unmarshal(b, &t) != nil || providerhttp.Field(t, "type") != "function" || json.Unmarshal(t["function"], &f) != nil || providerhttp.Field(f, "name") == "" {
				return bad("tool_choice 无效")
			}
			choice = map[string]any{"type": "tool", "name": providerhttp.Field(f, "name")}
		}
	}
	if b := in["parallel_tool_calls"]; len(b) > 0 {
		var parallel bool
		if json.Unmarshal(b, &parallel) != nil {
			return bad("parallel_tool_calls 必须为布尔值")
		}
		if choice == nil {
			choice = map[string]any{"type": "auto"}
		}
		choice["disable_parallel_tool_use"] = !parallel
	}
	if choice != nil {
		out["tool_choice"] = choice
	}
	return out, nil
}

type messageBlock struct {
	kind, id, name, args, signature, initial, thought, redacted string
	index                                                       int
	stopped                                                     bool
}

func messagesStream(ctx context.Context, r io.Reader, model string, emit func([]byte) error) error {
	c := providerhttp.Chunks{Model: model, Emit: emit}
	blocks := map[int]*messageBlock{}
	toolCount, toolBytes := 0, 0
	started, delta, observed := false, false, false
	finish := ""
	var input, output, cached, write int64
	readUsage := func(raw json.RawMessage, initial bool) error {
		var u map[string]json.RawMessage
		if json.Unmarshal(raw, &u) != nil || u == nil {
			return providerhttp.Broken("Claude")
		}
		for k, dst := range map[string]*int64{"input_tokens": &input, "output_tokens": &output, "cache_read_input_tokens": &cached, "cache_creation_input_tokens": &write} {
			if b := u[k]; len(b) > 0 {
				var n int64
				if json.Unmarshal(b, &n) != nil || n < 0 || n > 1<<48 {
					return providerhttp.Broken("Claude")
				}
				*dst = n
			} else if k == "output_tokens" || initial && k == "input_tokens" {
				return providerhttp.Broken("Claude")
			}
		}
		return nil
	}
	return providerhttp.SSE(ctx, r, "Claude", func(event string, b []byte) error {
		var o map[string]json.RawMessage
		if json.Unmarshal(b, &o) != nil || o == nil {
			return providerhttp.Broken("Claude")
		}
		typ := providerhttp.Field(o, "type")
		if typ == "" {
			typ = event
		}
		switch typ {
		case "ping":
			return nil
		case "error":
			return providerhttp.Broken("Claude")
		case "message_start":
			if started {
				return providerhttp.Broken("Claude")
			}
			var m map[string]json.RawMessage
			if json.Unmarshal(o["message"], &m) != nil || providerhttp.Field(m, "role") != "assistant" {
				return providerhttp.Broken("Claude")
			}
			if e := readUsage(m["usage"], true); e != nil {
				return e
			}
			c.ID = providerhttp.Field(m, "id")
			started = true
			return nil
		case "content_block_start":
			if !started || delta || len(blocks) >= 256 {
				return providerhttp.Broken("Claude")
			}
			var index int
			var block map[string]json.RawMessage
			if json.Unmarshal(o["index"], &index) != nil || index < 0 || blocks[index] != nil || json.Unmarshal(o["content_block"], &block) != nil {
				return providerhttp.Broken("Claude")
			}
			v := &messageBlock{kind: providerhttp.Field(block, "type"), index: -1}
			blocks[index] = v
			switch v.kind {
			case "text", "thinking":
				value := providerhttp.Field(block, "text")
				key := "content"
				if v.kind == "thinking" {
					value = providerhttp.Field(block, "thinking")
					v.thought = value
					key = "reasoning_content"
				}
				if value != "" {
					observed = true
					if e := c.Start(); e != nil {
						return e
					}
					return c.Send(map[string]string{key: value}, nil)
				}
			case "tool_use":
				v.id = providerhttp.Field(block, "id")
				v.name = providerhttp.Field(block, "name")
				if v.id == "" || v.name == "" || toolCount >= 128 {
					return providerhttp.Broken("Claude")
				}
				for _, old := range blocks {
					if old != v && old.id == v.id {
						return providerhttp.Broken("Claude")
					}
				}
				if initial := block["input"]; len(initial) > 0 {
					var obj map[string]json.RawMessage
					if json.Unmarshal(initial, &obj) != nil || obj == nil {
						return providerhttp.Broken("Claude")
					}
					if len(obj) > 0 {
						v.initial = string(initial)
					}
				}
				v.index = toolCount
				toolCount++
				observed = true
				if e := c.Start(); e != nil {
					return e
				}
				return c.Send(map[string]any{"tool_calls": []any{map[string]any{"index": v.index, "id": v.id, "type": "function", "function": map[string]string{"name": v.name, "arguments": ""}}}}, nil)
			case "redacted_thinking":
				v.redacted = providerhttp.Field(block, "data")
				if v.redacted == "" || len(v.redacted) > 4<<20 {
					return providerhttp.Broken("Claude")
				}
				observed = true
				return nil
			default:
				return providerhttp.Broken("Claude")
			}
			return nil
		case "content_block_delta":
			if !started || delta {
				return providerhttp.Broken("Claude")
			}
			var index int
			var d map[string]json.RawMessage
			if json.Unmarshal(o["index"], &index) != nil || json.Unmarshal(o["delta"], &d) != nil {
				return providerhttp.Broken("Claude")
			}
			v := blocks[index]
			if v == nil || v.stopped {
				return providerhttp.Broken("Claude")
			}
			kind := providerhttp.Field(d, "type")
			switch kind {
			case "text_delta", "thinking_delta":
				key, value := "content", providerhttp.Field(d, "text")
				want := "text"
				if kind == "thinking_delta" {
					key, value, want = "reasoning_content", providerhttp.Field(d, "thinking"), "thinking"
				}
				if want == "thinking" {
					if len(v.thought)+len(value) > 4<<20 {
						return providerhttp.Broken("Claude")
					}
					v.thought += value
				}
				if v.kind != want {
					return providerhttp.Broken("Claude")
				}
				if value != "" {
					observed = true
					if e := c.Start(); e != nil {
						return e
					}
					return c.Send(map[string]string{key: value}, nil)
				}
			case "signature_delta":
				if v.kind != "thinking" {
					return providerhttp.Broken("Claude")
				}
				value := providerhttp.Field(d, "signature")
				if len(v.signature)+len(value) > 1<<20 {
					return providerhttp.Broken("Claude")
				}
				v.signature += value
				if e := c.Start(); e != nil {
					return e
				}
				return c.Send(map[string]string{"reasoning_signature": value}, nil)
			case "input_json_delta":
				part := providerhttp.Field(d, "partial_json")
				if v.kind != "tool_use" || v.initial != "" || len(v.args)+len(part) > 4<<20 || toolBytes+len(part) > 8<<20 {
					return providerhttp.Broken("Claude")
				}
				v.args += part
				toolBytes += len(part)
				return c.Send(map[string]any{"tool_calls": []any{map[string]any{"index": v.index, "function": map[string]string{"arguments": part}}}}, nil)
			default:
				return providerhttp.Broken("Claude")
			}
			return nil
		case "content_block_stop":
			var index int
			if json.Unmarshal(o["index"], &index) != nil || blocks[index] == nil || blocks[index].stopped {
				return providerhttp.Broken("Claude")
			}
			v := blocks[index]
			if v.kind == "tool_use" {
				if v.args == "" {
					v.args = v.initial
					if v.args == "" {
						v.args = "{}"
					}
					if e := c.Send(map[string]any{"tool_calls": []any{map[string]any{"index": v.index, "function": map[string]string{"arguments": v.args}}}}, nil); e != nil {
						return e
					}
				}
				var args map[string]json.RawMessage
				if json.Unmarshal([]byte(v.args), &args) != nil || args == nil {
					return providerhttp.Broken("Claude")
				}
			}
			if v.kind == "thinking" || v.kind == "redacted_thinking" {
				var block map[string]string
				if v.kind == "thinking" {
					if v.signature == "" {
						return providerhttp.Broken("Claude")
					}
					block = map[string]string{"type": "thinking", "thinking": v.thought, "signature": v.signature}
				} else {
					block = map[string]string{"type": "redacted_thinking", "data": v.redacted}
				}
				if e := c.Start(); e != nil {
					return e
				}
				if e := c.Send(map[string]any{"anthropic_thinking_blocks": []any{block}}, nil); e != nil {
					return e
				}
			}
			v.stopped = true
			return nil
		case "message_delta":
			if !started || delta {
				return providerhttp.Broken("Claude")
			}
			for _, v := range blocks {
				if !v.stopped {
					return providerhttp.Broken("Claude")
				}
			}
			var d map[string]json.RawMessage
			if json.Unmarshal(o["delta"], &d) != nil {
				return providerhttp.Broken("Claude")
			}
			switch providerhttp.Field(d, "stop_reason") {
			case "end_turn", "stop_sequence":
				finish = "stop"
			case "tool_use":
				if toolCount == 0 {
					return providerhttp.Broken("Claude")
				}
				finish = "tool_calls"
			case "max_tokens", "model_context_window_exceeded":
				finish = "length"
			case "refusal":
				finish = "content_filter"
			default:
				return providerhttp.Broken("Claude")
			}
			if e := readUsage(o["usage"], false); e != nil {
				return e
			}
			delta = true
			return nil
		case "message_stop":
			if !started || !delta || !observed {
				return providerhttp.Broken("Claude")
			}
			if e := c.Usage(input+cached+write, output, cached, write); e != nil {
				return e
			}
			if e := c.Send(map[string]any{}, finish); e != nil {
				return e
			}
			return providerhttp.End
		default:
			return providerhttp.Broken("Claude")
		}
	})
}
