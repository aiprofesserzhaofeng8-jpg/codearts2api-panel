package providerhttp

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// NativeBody changes only the routing envelope; unknown fields and exact JSON
// numbers remain opaque. Native streams are also used for buffered consumers.
func NativeBody(r provider.Request, codex bool) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(r.Body, &obj) != nil || obj == nil {
		return nil, Invalid("Provider", "请求必须是 JSON 对象")
	}
	obj["model"], _ = json.Marshal(r.Model)
	obj["stream"] = json.RawMessage("true")
	if r.Protocol == provider.MessagesProtocol && len(obj["max_tokens"]) == 0 {
		obj["max_tokens"] = json.RawMessage("4096")
	}
	if codex {
		obj["store"] = json.RawMessage("false")
		var text string
		if json.Unmarshal(obj["input"], &text) == nil {
			obj["input"], _ = json.Marshal([]any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]string{"type": "input_text", "text": text}}}})
		}
		if len(obj["instructions"]) == 0 {
			obj["instructions"] = json.RawMessage(`""`)
		}
		// Preserve the legacy Codex adapter's output-limit semantics: the
		// subscription backend ignores this control (API-key Responses keeps it).
		delete(obj, "max_output_tokens")
		for _, key := range []string{"temperature", "top_p"} {
			if len(obj[key]) > 0 && string(obj[key]) != "null" {
				return nil, Invalid("ChatGPT", "Codex 不支持 "+key)
			}
		}
	}
	return json.Marshal(obj)
}

// NativeSSE retains original events and requires an authoritative terminal event.
// No converted conversation or Chat stream is constructed for usage accounting.
func NativeSSE(ctx context.Context, r io.Reader, p provider.Protocol, name string, emit func([]byte) error) error {
	started := false
	stopped := false
	blocks := map[int]bool{}
	return SSE(ctx, r, name, func(event string, b []byte) error {
		var obj map[string]json.RawMessage
		if json.Unmarshal(b, &obj) != nil || obj == nil {
			return Broken(name)
		}
		typ := Field(obj, "type")
		if typ == "" && event != "" {
			typ = event
			obj["type"], _ = json.Marshal(typ)
			b, _ = json.Marshal(obj)
		}
		if event != "" && typ != "" && event != typ {
			return Broken(name)
		}
		if typ == "error" || typ == "response.failed" || len(obj["error"]) > 0 && string(obj["error"]) != "null" {
			return &provider.Error{Status: 502, Message: name + " 返回失败事件"}
		}
		terminal := false
		switch p {
		case provider.MessagesProtocol:
			if typ == "message_start" {
				if started {
					return Broken(name)
				}
				started = true
			}
			if typ == "content_block_start" || typ == "content_block_stop" || typ == "content_block_delta" {
				var index int
				if json.Unmarshal(obj["index"], &index) != nil || index < 0 {
					return Broken(name)
				}
				if typ == "content_block_start" {
					if blocks[index] {
						return Broken(name)
					}
					blocks[index] = true
				} else if !blocks[index] {
					return Broken(name)
				} else if typ == "content_block_stop" {
					delete(blocks, index)
				}
			}
			if typ == "message_delta" {
				var delta map[string]json.RawMessage
				if json.Unmarshal(obj["delta"], &delta) != nil || Field(delta, "stop_reason") == "" || len(blocks) > 0 {
					return Broken(name)
				}
				stopped = true
			}
			if typ == "message_stop" && !stopped {
				return Broken(name)
			}
			terminal = typ == "message_stop"
		case provider.ResponsesProtocol:
			if typ == "response.created" {
				started = true
			}
			terminal = typ == "response.completed" || typ == "response.incomplete"
			if terminal {
				var response map[string]json.RawMessage
				if json.Unmarshal(obj["response"], &response) != nil || response == nil {
					return Broken(name)
				}
				status := Field(response, "status")
				if status != "completed" && status != "incomplete" {
					return Broken(name)
				}
			}
		default:
			return Broken(name)
		}
		if !started && typ != "ping" {
			return Broken(name)
		}
		if strings.ContainsAny(typ, "\r\n") || typ == "" {
			return Broken(name)
		}
		if err := emit(b); err != nil {
			return err
		}
		if terminal {
			return End
		}
		return nil
	})
}
