package requestpolicy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
)

// DefaultSystemPrompt is adapted from workbuddy2api-panel (MIT; LICENSE.workbuddy).
//
//go:embed defaultprompt.md
var DefaultSystemPrompt string

const FallbackSystemPrompt = "You are a helpful assistant. Respond in the user's language, follow the user's instructions, and be direct and concise."

// PrepareBody changes only system/developer messages and known fingerprint text.
// RawMessage preserves unrelated numeric precision, tools, schemas and options.
func PrepareBody(body []byte, mode, prompt string, sanitize bool, profile string) ([]byte, error) {
	if mode != "custom" && mode != "passthrough" {
		return nil, fmt.Errorf("提示词模式无效")
	}
	if mode == "passthrough" && !sanitize {
		return body, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("请求必须是 JSON 对象")
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(obj["messages"], &messages); err != nil {
		return nil, fmt.Errorf("messages 格式无效")
	}
	kept := make([]map[string]json.RawMessage, 0, len(messages)+1)
	if mode == "custom" {
		if prompt == "" {
			prompt = DefaultSystemPrompt
		}
		text, _ := json.Marshal(prompt)
		kept = append(kept, map[string]json.RawMessage{"role": json.RawMessage(`"system"`), "content": text})
	}
	for _, message := range messages {
		var role string
		_ = json.Unmarshal(message["role"], &role)
		if mode == "custom" && (role == "system" || role == "developer") {
			continue
		}
		kept = append(kept, message)
	}
	if sanitize {
		cleaner := fingerprintCleaner{workBuddy: profile == "workbuddy"}
		for _, message := range kept {
			// Decode only the targeted fields, leaving tools/schema and numeric options untouched.
			for _, field := range []string{"content", "reasoning_content", "reasoning", "tool_calls"} {
				raw, exists := message[field]
				if !exists {
					continue
				}
				var value any
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.UseNumber()
				if err := decoder.Decode(&value); err != nil {
					return nil, fmt.Errorf("消息字段格式无效")
				}
				changed := false
				if field == "tool_calls" {
					changed = cleaner.sanitizeToolCalls(value)
				} else {
					value, changed = cleaner.sanitizeContent(value)
				}
				if changed {
					message[field], _ = json.Marshal(value)
				}
			}
		}
	}
	obj["messages"], _ = json.Marshal(kept)
	return json.Marshal(obj)
}
