package requestpolicy

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// DefaultSystemPrompt is adapted from workbuddy2api-panel (MIT; licenses/THIRD-PARTY-MIT.txt).
//
//go:embed defaultprompt.md
var DefaultSystemPrompt string

const FallbackSystemPrompt = "You are a helpful assistant. Respond in the user's language, follow the user's instructions, and be direct and concise."

// PrepareBody applies the explicitly selected system-prompt policy. Cleaning is
// restricted to system/developer content; business messages, reasoning and tool
// arguments are opaque. An unchanged request returns its original bytes.
func PrepareBody(body []byte, mode, prompt string, sanitize bool) ([]byte, error) {
	if mode != "custom" && mode != "passthrough" && mode != "append" {
		return nil, fmt.Errorf("提示词模式无效")
	}
	if (mode == "passthrough" || mode == "append" && prompt == "") && !sanitize {
		return body, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("请求必须是 JSON 对象")
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(obj["messages"], &messages); err != nil {
		return nil, fmt.Errorf("messages 格式无效")
	}
	changed := mode == "custom" || mode == "append" && prompt != ""
	kept := make([]json.RawMessage, 0, len(messages)+1)
	if mode == "custom" {
		if prompt == "" {
			prompt = DefaultSystemPrompt
		}
		if sanitize {
			prompt = sanitizeText(prompt)
		}
		raw, _ := json.Marshal(struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{"system", prompt})
		kept = append(kept, raw)
	}
	for _, raw := range messages {
		// Read only the role; do not decode/copy large user text into strings or maps.
		var header struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return nil, fmt.Errorf("消息格式无效")
		}
		system := header.Role == "system" || header.Role == "developer"
		if mode == "custom" && system {
			continue
		}
		if sanitize && system {
			var message map[string]json.RawMessage
			if err := json.Unmarshal(raw, &message); err != nil {
				return nil, fmt.Errorf("消息格式无效")
			}
			content, cleaned := sanitizeSystemContent(message["content"])
			if cleaned {
				message["content"] = content
				raw, _ = json.Marshal(message)
				changed = true
			}
		}
		kept = append(kept, raw)
	}
	if mode == "append" && prompt != "" {
		if sanitize {
			prompt = sanitizeText(prompt)
		}
		added, _ := json.Marshal(struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{"system", prompt})
		// Insert after existing instruction messages, before conversation content.
		index := 0
		for i, raw := range kept {
			var h struct{ Role string }
			_ = json.Unmarshal(raw, &h)
			if h.Role == "system" || h.Role == "developer" {
				index = i + 1
			}
		}
		kept = append(kept, nil)
		copy(kept[index+1:], kept[index:])
		kept[index] = added
	}
	if !changed {
		return body, nil
	}
	obj["messages"], _ = json.Marshal(kept)
	return json.Marshal(obj)
}
