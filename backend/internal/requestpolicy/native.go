package requestpolicy

import (
	"codearts2api/internal/provider"
	"encoding/json"
	"fmt"
)

// PrepareProtocolBody edits prompt fields only, without round-tripping native
// tools, signatures, content or unknown controls through a Chat request.
func PrepareProtocolBody(body []byte, p provider.Protocol, mode, prompt string, sanitize bool) ([]byte, error) {
	if p == "" || p == provider.ChatProtocol {
		return PrepareBody(body, mode, prompt, sanitize)
	}
	if mode != "custom" && mode != "passthrough" && mode != "append" {
		return nil, fmt.Errorf("提示词模式无效")
	}
	if (mode == "passthrough" || mode == "append" && prompt == "") && !sanitize {
		return body, nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil || obj == nil {
		return nil, fmt.Errorf("请求必须是 JSON 对象")
	}
	field := "system"
	if p == provider.ResponsesProtocol {
		field = "instructions"
	}
	if mode == "custom" {
		if prompt == "" {
			prompt = DefaultSystemPrompt
		}
		if sanitize {
			prompt = sanitizeText(prompt)
		}
		obj[field], _ = json.Marshal(prompt)
	} else if sanitize {
		if cleaned, changed := sanitizeSystemContent(obj[field]); changed {
			obj[field] = cleaned
		}
	}
	if p == provider.ResponsesProtocol {
		var input []json.RawMessage
		if json.Unmarshal(obj["input"], &input) == nil {
			kept := make([]json.RawMessage, 0, len(input))
			for _, item := range input {
				var m map[string]json.RawMessage
				if json.Unmarshal(item, &m) != nil {
					return nil, fmt.Errorf("input 格式无效")
				}
				var role string
				_ = json.Unmarshal(m["role"], &role)
				if role == "system" || role == "developer" {
					if mode == "custom" {
						continue
					}
					if cleaned, changed := sanitizeSystemContent(m["content"]); sanitize && changed {
						m["content"] = cleaned
						item, _ = json.Marshal(m)
					}
				}
				kept = append(kept, item)
			}
			obj["input"], _ = json.Marshal(kept)
		}
	}
	if mode == "append" && prompt != "" {
		if sanitize {
			prompt = sanitizeText(prompt)
		}
		if p == provider.MessagesProtocol {
			var text string
			var blocks []json.RawMessage
			current := obj[field]
			if len(current) > 0 && string(current) != "null" {
				if json.Unmarshal(current, &text) == nil {
					if text != "" {
						block, _ := json.Marshal(map[string]string{"type": "text", "text": text})
						blocks = append(blocks, block)
					}
				} else if json.Unmarshal(current, &blocks) != nil {
					return nil, fmt.Errorf("system 格式无效")
				}
			}
			block, _ := json.Marshal(map[string]string{"type": "text", "text": prompt})
			blocks = append(blocks, block)
			obj[field], _ = json.Marshal(blocks)
		} else {
			// Keep existing input (including developer instructions) byte-for-byte.
			// Add to instructions without replacing the original instruction text.
			var text string
			if len(obj[field]) > 0 && string(obj[field]) != "null" && json.Unmarshal(obj[field], &text) != nil {
				return nil, fmt.Errorf("instructions 格式无效")
			}
			if text != "" {
				text += "\n\n"
			}
			obj[field], _ = json.Marshal(text + prompt)
		}
	}
	return json.Marshal(obj)
}
func IsProtocolSummary(body []byte, p provider.Protocol, explicit bool) bool {
	if p == "" || p == provider.ChatProtocol {
		return IsSummaryRequest(body, explicit)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return false
	}
	var messages []json.RawMessage
	field := "system"
	if p == provider.ResponsesProtocol {
		field = "instructions"
		_ = json.Unmarshal(obj["input"], &messages)
	}
	if len(obj[field]) > 0 {
		m, _ := json.Marshal(map[string]any{"role": "system", "content": obj[field]})
		messages = append(messages, m)
	}
	projection, _ := json.Marshal(map[string]any{"tools": obj["tools"], "messages": messages})
	return IsSummaryRequest(projection, explicit)
}
