package protocol

import (
	"encoding/json"
	"fmt"
)

// Requirements is a capability projection, not a normalized conversation.
// Walk only actual message content, never schemas or arbitrary tool arguments.
type Requirements struct {
	Image, Tools bool
	MaxOutput    int64
}

func InspectRequirements(body []byte, p string) (Requirements, error) {
	var obj map[string]json.RawMessage
	var out Requirements
	if json.Unmarshal(body, &obj) != nil || obj == nil {
		return out, fmt.Errorf("请求必须是 JSON 对象")
	}
	var tools []json.RawMessage
	if len(obj["tools"]) > 0 && json.Unmarshal(obj["tools"], &tools) != nil {
		return out, fmt.Errorf("tools 格式无效")
	}
	out.Tools = len(tools) > 0
	for _, k := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		var n int64
		if json.Unmarshal(obj[k], &n) == nil && n > out.MaxOutput {
			out.MaxOutput = n
		}
	}
	field := "messages"
	if p == "responses" {
		field = "input"
	}
	var messages []struct {
		Type, Role string
		Content    json.RawMessage
		ToolCalls  []json.RawMessage `json:"tool_calls"`
	}
	if p == "responses" {
		var text string
		if json.Unmarshal(obj[field], &text) == nil {
			return out, nil
		}
	}
	if json.Unmarshal(obj[field], &messages) != nil {
		return out, fmt.Errorf("%s 格式无效", field)
	}
	for _, m := range messages {
		out.Tools = out.Tools || m.Role == "tool" || len(m.ToolCalls) > 0 || m.Type == "function_call" || m.Type == "function_call_output" || m.Type == "custom_tool_call" || m.Type == "custom_tool_call_output"
		var parts []struct {
			Type    string
			Content json.RawMessage
		}
		if json.Unmarshal(m.Content, &parts) != nil {
			continue
		}
		for _, part := range parts {
			out.Image = out.Image || part.Type == "image" || part.Type == "image_url" || part.Type == "input_image"
			out.Tools = out.Tools || part.Type == "tool_use" || part.Type == "tool_result"
			if part.Type == "tool_result" {
				var nested []struct{ Type string }
				if json.Unmarshal(part.Content, &nested) == nil {
					for _, v := range nested {
						out.Image = out.Image || v.Type == "image" || v.Type == "image_url"
					}
				}
			}
		}
	}
	return out, nil
}
