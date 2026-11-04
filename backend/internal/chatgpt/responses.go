package chatgpt

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

func responsesBody(r provider.Request, codex bool) (any, error) {
	bad := func(text string) (any, error) { return nil, providerhttp.Invalid("ChatGPT", text) }
	var in map[string]json.RawMessage
	if json.Unmarshal(r.Body, &in) != nil {
		return bad("请求格式无效")
	}
	for _, key := range []string{"seed", "stop", "frequency_penalty", "presence_penalty", "logprobs", "top_logprobs"} {
		if b := in[key]; len(b) > 0 && string(b) != "null" {
			return bad("Responses 转换不支持 " + key)
		}
	}
	if b := in["n"]; len(b) > 0 && string(b) != "1" {
		return bad("仅支持 n=1")
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(in["messages"], &messages) != nil || len(messages) == 0 {
		return bad("缺少 messages")
	}
	var items []any
	var instructions []string
	for _, m := range messages {
		role := providerhttp.Field(m, "role")
		if role == "tool" {
			id := providerhttp.Field(m, "tool_call_id")
			if id == "" {
				return bad("工具结果缺少 tool_call_id")
			}
			var content string
			if json.Unmarshal(m["content"], &content) != nil {
				return bad("工具结果目前仅支持文本")
			}
			items = append(items, map[string]any{"type": "function_call_output", "call_id": id, "output": content})
			continue
		}
		if role != "user" && role != "assistant" && role != "system" && role != "developer" {
			return bad("未知消息角色")
		}
		var text string
		var blocks []map[string]json.RawMessage
		var parts []any
		if b := m["content"]; len(b) > 0 && string(b) != "null" {
			if json.Unmarshal(b, &text) == nil {
				if text != "" {
					typ := "input_text"
					if role == "assistant" {
						typ = "output_text"
					}
					parts = append(parts, map[string]string{"type": typ, "text": text})
				}
			} else if json.Unmarshal(b, &blocks) == nil {
				for _, block := range blocks {
					switch providerhttp.Field(block, "type") {
					case "text":
						typ := "input_text"
						if role == "assistant" {
							typ = "output_text"
						}
						parts = append(parts, map[string]string{"type": typ, "text": providerhttp.Field(block, "text")})
					case "image_url":
						if role != "user" {
							return bad("图片只支持用户消息")
						}
						var image map[string]json.RawMessage
						if json.Unmarshal(block["image_url"], &image) != nil || providerhttp.Field(image, "url") == "" {
							return bad("图片地址无效")
						}
						image["type"] = json.RawMessage(`"input_image"`)
						image["image_url"] = image["url"]
						delete(image, "url")
						parts = append(parts, image)
					default:
						return bad("暂不支持该内容块")
					}
				}
			} else {
				return bad("content 格式无效")
			}
		}
		if role == "system" {
			if len(blocks) > 0 {
				for _, b := range blocks {
					if providerhttp.Field(b, "type") != "text" {
						return bad("system 仅支持文本")
					}
					instructions = append(instructions, providerhttp.Field(b, "text"))
				}
			} else {
				instructions = append(instructions, text)
			}
		} else if len(parts) > 0 {
			items = append(items, map[string]any{"type": "message", "role": role, "content": parts})
		}
		if refusal := providerhttp.Field(m, "refusal"); refusal != "" {
			if role != "assistant" {
				return bad("refusal 只支持助手消息")
			}
			items = append(items, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "refusal", "refusal": refusal}}})
		}
		var calls []struct {
			ID, Type string
			Function struct{ Name, Arguments string }
		}
		if b := m["tool_calls"]; len(b) > 0 && string(b) != "null" && json.Unmarshal(b, &calls) != nil {
			return bad("工具调用格式无效")
		}
		for _, call := range calls {
			var args map[string]json.RawMessage
			if role != "assistant" || call.ID == "" || call.Type != "function" || call.Function.Name == "" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
				return bad("工具参数必须为完整 JSON 对象")
			}
			items = append(items, map[string]string{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments})
		}
	}
	out := map[string]any{"model": r.Model, "input": items, "instructions": strings.Join(instructions, "\n\n"), "store": false, "stream": true}
	for _, key := range []string{"temperature", "top_p"} {
		if b := in[key]; len(b) > 0 && string(b) != "null" {
			if codex {
				return bad("Codex 不支持 " + key)
			}
			out[key] = b
		}
	}
	if !codex {
		if b := in["max_completion_tokens"]; len(b) > 0 {
			out["max_output_tokens"] = b
		} else if b := in["max_tokens"]; len(b) > 0 {
			out["max_output_tokens"] = b
		}
	}
	if effort := providerhttp.Field(in, "reasoning_effort"); effort != "" {
		out["reasoning"] = map[string]string{"effort": effort, "summary": "auto"}
	}
	if b := in["thinking"]; len(b) > 0 && string(b) != "null" {
		var t map[string]json.RawMessage
		if json.Unmarshal(b, &t) != nil || providerhttp.Field(t, "type") != "disabled" {
			return bad("thinking 请改用 reasoning_effort")
		}
		out["reasoning"] = map[string]string{"effort": "none"}
	}
	if b := in["parallel_tool_calls"]; len(b) > 0 {
		out["parallel_tool_calls"] = b
	}
	if b := in["response_format"]; len(b) > 0 && string(b) != "null" {
		var format map[string]json.RawMessage
		if json.Unmarshal(b, &format) != nil {
			return bad("response_format 格式无效")
		}
		if providerhttp.Field(format, "type") == "json_schema" {
			var schema map[string]json.RawMessage
			if json.Unmarshal(format["json_schema"], &schema) != nil {
				return bad("json_schema 格式无效")
			}
			schema["type"] = json.RawMessage(`"json_schema"`)
			out["text"] = map[string]any{"format": schema}
		} else {
			out["text"] = map[string]any{"format": format}
		}
	}
	var tools []map[string]json.RawMessage
	if b := in["tools"]; len(b) > 0 && string(b) != "null" && json.Unmarshal(b, &tools) != nil {
		return bad("tools 格式无效")
	}
	if len(tools) > 0 {
		var converted []any
		for _, tool := range tools {
			if providerhttp.Field(tool, "type") != "function" {
				return bad("仅支持 function 工具")
			}
			var fn map[string]json.RawMessage
			if json.Unmarshal(tool["function"], &fn) != nil || providerhttp.Field(fn, "name") == "" {
				return bad("工具定义无效")
			}
			fn["type"] = json.RawMessage(`"function"`)
			converted = append(converted, fn)
		}
		out["tools"] = converted
	}
	if b := in["tool_choice"]; len(b) > 0 && string(b) != "null" {
		var choice string
		if json.Unmarshal(b, &choice) == nil {
			out["tool_choice"] = choice
		} else {
			var ch map[string]json.RawMessage
			var fn map[string]json.RawMessage
			if json.Unmarshal(b, &ch) != nil || json.Unmarshal(ch["function"], &fn) != nil || providerhttp.Field(fn, "name") == "" {
				return bad("tool_choice 格式无效")
			}
			out["tool_choice"] = map[string]string{"type": "function", "name": providerhttp.Field(fn, "name")}
		}
	}
	return out, nil
}

type responseTool struct {
	ID, Name, Args string
	Index          int
	Done           bool
}

func responsesStream(ctx context.Context, r io.Reader, model string, emit func([]byte) error) error {
	c := providerhttp.Chunks{Model: model, Emit: emit}
	tools := map[string]*responseTool{}
	observed := false
	toolBytes := 0
	toolStart := func(item map[string]json.RawMessage) (*responseTool, error) {
		id := providerhttp.Field(item, "id")
		call := providerhttp.Field(item, "call_id")
		name := providerhttp.Field(item, "name")
		if id == "" {
			id = call
		}
		if t := tools[id]; t != nil {
			if call != t.ID || name != t.Name {
				return nil, providerhttp.Broken("ChatGPT")
			}
			return t, nil
		}
		if id == "" || call == "" || name == "" || len(tools) >= 128 {
			return nil, providerhttp.Broken("ChatGPT")
		}
		t := &responseTool{ID: call, Name: name, Index: len(tools)}
		for _, old := range tools {
			if old.ID == call {
				return nil, providerhttp.Broken("ChatGPT")
			}
		}
		tools[id] = t
		observed = true
		if e := c.Start(); e != nil {
			return nil, e
		}
		e := c.Send(map[string]any{"tool_calls": []any{map[string]any{"index": t.Index, "id": call, "type": "function", "function": map[string]string{"name": name, "arguments": ""}}}}, nil)
		return t, e
	}
	appendArgs := func(t *responseTool, part string) error {
		if t == nil || t.Done || len(t.Args)+len(part) > 4<<20 || toolBytes+len(part) > 8<<20 {
			return providerhttp.Broken("ChatGPT")
		}
		t.Args += part
		toolBytes += len(part)
		return c.Send(map[string]any{"tool_calls": []any{map[string]any{"index": t.Index, "function": map[string]string{"arguments": part}}}}, nil)
	}
	validate := func(t *responseTool) error {
		var o map[string]json.RawMessage
		if t == nil || json.Unmarshal([]byte(t.Args), &o) != nil || o == nil {
			return providerhttp.Broken("ChatGPT")
		}
		t.Done = true
		return nil
	}
	return providerhttp.SSE(ctx, r, "ChatGPT", func(event string, b []byte) error {
		var o map[string]json.RawMessage
		if json.Unmarshal(b, &o) != nil || o == nil {
			return providerhttp.Broken("ChatGPT")
		}
		typ := providerhttp.Field(o, "type")
		if typ == "" {
			typ = event
		}
		switch typ {
		case "error", "response.failed", "response.cancelled":
			return providerhttp.Broken("ChatGPT")
		case "response.created":
			var response map[string]json.RawMessage
			if json.Unmarshal(o["response"], &response) == nil {
				c.ID = providerhttp.Field(response, "id")
			}
		case "response.output_text.delta", "response.refusal.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			text := providerhttp.Field(o, "delta")
			if text == "" {
				return nil
			}
			observed = true
			key := "content"
			if typ == "response.refusal.delta" {
				key = "refusal"
			}
			if strings.Contains(typ, "reasoning") {
				key = "reasoning_content"
			}
			if e := c.Start(); e != nil {
				return e
			}
			return c.Send(map[string]string{key: text}, nil)
		case "response.output_item.added":
			var item map[string]json.RawMessage
			if json.Unmarshal(o["item"], &item) != nil {
				return providerhttp.Broken("ChatGPT")
			}
			if providerhttp.Field(item, "type") == "function_call" {
				_, e := toolStart(item)
				return e
			}
		case "response.function_call_arguments.delta":
			return appendArgs(tools[providerhttp.Field(o, "item_id")], providerhttp.Field(o, "delta"))
		case "response.function_call_arguments.done":
			t := tools[providerhttp.Field(o, "item_id")]
			if t == nil {
				return providerhttp.Broken("ChatGPT")
			}
			full := providerhttp.Field(o, "arguments")
			if full != "" && t.Args == "" {
				if e := appendArgs(t, full); e != nil {
					return e
				}
			} else if full != "" && full != t.Args {
				return providerhttp.Broken("ChatGPT")
			}
			return validate(t)
		case "response.output_item.done":
			var item map[string]json.RawMessage
			if json.Unmarshal(o["item"], &item) != nil {
				return providerhttp.Broken("ChatGPT")
			}
			if providerhttp.Field(item, "type") == "function_call" {
				t, e := toolStart(item)
				if e != nil {
					return e
				}
				full := providerhttp.Field(item, "arguments")
				if t.Args == "" && full != "" {
					if e = appendArgs(t, full); e != nil {
						return e
					}
				} else if full != "" && full != t.Args {
					return providerhttp.Broken("ChatGPT")
				}
				return validate(t)
			}
		case "response.completed", "response.incomplete":
			var response map[string]json.RawMessage
			if json.Unmarshal(o["response"], &response) != nil || response == nil || len(response["error"]) > 0 && string(response["error"]) != "null" {
				return providerhttp.Broken("ChatGPT")
			}
			status := providerhttp.Field(response, "status")
			if typ == "response.completed" && status != "completed" || typ == "response.incomplete" && status != "incomplete" {
				return providerhttp.Broken("ChatGPT")
			}
			if !observed {
				var output []map[string]json.RawMessage
				if json.Unmarshal(response["output"], &output) != nil {
					return providerhttp.Broken("ChatGPT")
				}
				for _, item := range output {
					switch providerhttp.Field(item, "type") {
					case "function_call":
						t, e := toolStart(item)
						if e != nil {
							return e
						}
						if e = appendArgs(t, providerhttp.Field(item, "arguments")); e != nil {
							return e
						}
						if e = validate(t); e != nil {
							return e
						}
					case "message", "reasoning":
						key := "content"
						partsKey := "content"
						if providerhttp.Field(item, "type") == "reasoning" {
							key = "reasoning_content"
							partsKey = "summary"
						}
						var parts []map[string]json.RawMessage
						if json.Unmarshal(item[partsKey], &parts) != nil {
							return providerhttp.Broken("ChatGPT")
						}
						for _, part := range parts {
							text := providerhttp.Field(part, "text")
							k := key
							if providerhttp.Field(part, "type") == "refusal" {
								text = providerhttp.Field(part, "refusal")
								k = "refusal"
							}
							if text != "" {
								observed = true
								if e := c.Start(); e != nil {
									return e
								}
								if e := c.Send(map[string]string{k: text}, nil); e != nil {
									return e
								}
							}
						}
					}
				}
			}
			if !observed {
				return providerhttp.Broken("ChatGPT")
			}
			finish := "stop"
			if len(tools) > 0 {
				finish = "tool_calls"
				for _, t := range tools {
					if !t.Done {
						return providerhttp.Broken("ChatGPT")
					}
				}
			}
			if typ == "response.incomplete" {
				var detail map[string]json.RawMessage
				_ = json.Unmarshal(response["incomplete_details"], &detail)
				switch providerhttp.Field(detail, "reason") {
				case "max_output_tokens":
					finish = "length"
				case "content_filter":
					finish = "content_filter"
				default:
					return providerhttp.Broken("ChatGPT")
				}
			}
			if u := response["usage"]; len(u) > 0 && string(u) != "null" {
				var usage struct {
					Input   *int64 `json:"input_tokens"`
					Output  *int64 `json:"output_tokens"`
					Details struct {
						Cached int64 `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				}
				if json.Unmarshal(u, &usage) != nil || usage.Input == nil || usage.Output == nil {
					return providerhttp.Broken("ChatGPT")
				}
				if e := c.Usage(*usage.Input, *usage.Output, usage.Details.Cached, 0); e != nil {
					return e
				}
			}
			if e := c.Send(map[string]any{}, finish); e != nil {
				return e
			}
			return providerhttp.End
		}
		return nil
	})
}
