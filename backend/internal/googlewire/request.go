// SPDX-License-Identifier: LGPL-3.0-only
// Shared native wire conversion extracted from internal/gemini; see UPSTREAM.md.
package googlewire

import (
	"bytes"
	"codearts2api/internal/providerhttp"
	"encoding/base64"
	"encoding/json"
	"strings"
)

func nativeParts(raw json.RawMessage) ([]any, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []any{map[string]any{"text": text}}, nil
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil, providerhttp.Invalid("Google native", "content 格式不支持")
	}
	out := []any{}
	for _, p := range parts {
		switch providerhttp.Field(p, "type") {
		case "text":
			if json.Unmarshal(p["text"], &text) != nil {
				return nil, providerhttp.Invalid("Google native", "text 无效")
			}
			out = append(out, map[string]any{"text": text})
		case "image_url":
			var img struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(p["image_url"], &img) != nil {
				return nil, providerhttp.Invalid("Google native", "图片格式无效")
			}
			header, data, ok := strings.Cut(img.URL, ",")
			if !ok || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") {
				return nil, providerhttp.Invalid("Google native", "CLI 图片需要内嵌 base64，未下载或丢弃远程图片")
			}
			if _, err := base64.StdEncoding.DecodeString(data); err != nil {
				return nil, providerhttp.Invalid("Google native", "图片 base64 无效")
			}
			out = append(out, map[string]any{"inlineData": map[string]string{"mimeType": strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), "data": data}})
		default:
			return nil, providerhttp.Invalid("Google native", "CLI 暂不支持该内容块，未静默丢弃")
		}
	}
	return out, nil
}

func Encode(body []byte, model string) (json.RawMessage, error) {
	var in map[string]json.RawMessage
	if json.Unmarshal(body, &in) != nil || in == nil {
		return nil, providerhttp.Invalid("Google native", "请求格式无效")
	}
	for _, k := range []string{"audio", "modalities", "extra_body", "prediction"} {
		if v := in[k]; len(v) > 0 && string(v) != "null" {
			return nil, providerhttp.Invalid("Google native", "CLI 暂不支持参数 "+k+"，未静默忽略")
		}
	}
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Calls   []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
			Extra struct {
				Google struct {
					Signature string `json:"thought_signature"`
				} `json:"google"`
			} `json:"extra_content"`
		} `json:"tool_calls"`
		CallID string `json:"tool_call_id"`
	}
	if json.Unmarshal(in["messages"], &messages) != nil || len(messages) == 0 {
		return nil, providerhttp.Invalid("Google native", "缺少 messages")
	}
	out := map[string]any{}
	contents := []any{}
	system := []any{}
	names := map[string]string{}
	for _, m := range messages {
		parts, err := nativeParts(m.Content)
		if err != nil {
			return nil, err
		}
		if m.Role == "system" || m.Role == "developer" {
			for _, p := range parts {
				if _, ok := p.(map[string]any)["text"]; !ok {
					return nil, providerhttp.Invalid("Google native", "system 只支持文本")
				}
			}
			system = append(system, parts...)
			continue
		}
		role := "user"
		switch m.Role {
		case "assistant":
			role = "model"
			for _, call := range m.Calls {
				args := json.RawMessage(call.Function.Arguments)
				if call.ID == "" || call.Function.Name == "" || !json.Valid(args) || !strings.HasPrefix(strings.TrimSpace(string(args)), "{") {
					return nil, providerhttp.Invalid("Google native", "工具调用缺少名称、ID 或 JSON 对象参数")
				}
				names[call.ID] = call.Function.Name
				p := map[string]any{"functionCall": map[string]any{"name": call.Function.Name, "args": args}}
				if call.Extra.Google.Signature != "" {
					p["thoughtSignature"] = call.Extra.Google.Signature
				}
				parts = append(parts, p)
			}
		case "tool":
			name := names[m.CallID]
			if name == "" {
				return nil, providerhttp.Invalid("Google native", "工具结果缺少对应调用")
			}
			texts := []string{}
			media := []any{}
			for _, p := range parts {
				pm := p.(map[string]any)
				if text, ok := pm["text"].(string); ok {
					texts = append(texts, text)
				} else {
					media = append(media, p)
				}
			}
			parts = []any{map[string]any{"functionResponse": map[string]any{"name": name, "response": map[string]any{"content": strings.Join(texts, "\n")}}}}
			parts = append(parts, media...)
		case "user":
		default:
			return nil, providerhttp.Invalid("Google native", "不支持该消息角色")
		}
		if len(parts) > 0 {
			contents = append(contents, map[string]any{"role": role, "parts": parts})
		}
	}
	if len(contents) == 0 {
		return nil, providerhttp.Invalid("Google native", "缺少有效消息")
	}
	out["contents"] = contents
	if len(system) > 0 {
		out["systemInstruction"] = map[string]any{"parts": system}
	}
	config := map[string]json.RawMessage{}
	for _, pair := range [][2]string{{"temperature", "temperature"}, {"top_p", "topP"}, {"max_tokens", "maxOutputTokens"}, {"max_completion_tokens", "maxOutputTokens"}} {
		from, to := pair[0], pair[1]
		if v := in[from]; len(v) > 0 && string(v) != "null" {
			config[to] = v
		}
	}
	if v := in["stop"]; len(v) > 0 && string(v) != "null" {
		var text string
		if json.Unmarshal(v, &text) == nil {
			v, _ = json.Marshal([]string{text})
		}
		config["stopSequences"] = v
	}
	if n := in["n"]; len(n) > 0 && string(n) != "null" && string(n) != "1" {
		return nil, providerhttp.Invalid("Google native", "仅支持 n=1")
	}
	var tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	if v := in["tools"]; len(v) > 0 && string(v) != "null" {
		if json.Unmarshal(v, &tools) != nil {
			return nil, providerhttp.Invalid("Google native", "tools 格式无效")
		}
	}
	declarations := []any{}
	for _, t := range tools {
		if t.Type != "function" || t.Function.Name == "" {
			return nil, providerhttp.Invalid("Google native", "CLI 只支持 function 工具")
		}
		var schema any = map[string]any{"type": "object", "properties": map[string]any{}}
		if len(t.Function.Parameters) > 0 {
			d := json.NewDecoder(bytes.NewReader(t.Function.Parameters))
			d.UseNumber()
			if d.Decode(&schema) != nil {
				return nil, providerhttp.Invalid("Google native", "工具 schema 无效")
			}
		}
		declarations = append(declarations, map[string]any{"name": t.Function.Name, "description": t.Function.Description, "parameters": cleanToolSchema(schema)})
	}
	if len(declarations) > 0 {
		out["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
	}
	if v := in["tool_choice"]; len(v) > 0 && string(v) != "null" {
		var choice string
		fc := map[string]any{}
		if json.Unmarshal(v, &choice) == nil {
			switch choice {
			case "auto":
				fc["mode"] = "AUTO"
			case "none":
				fc["mode"] = "NONE"
			case "required":
				fc["mode"] = "ANY"
			default:
				return nil, providerhttp.Invalid("Google native", "tool_choice 无效")
			}
		} else {
			var chosen struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if json.Unmarshal(v, &chosen) != nil || chosen.Type != "function" || chosen.Function.Name == "" {
				return nil, providerhttp.Invalid("Google native", "tool_choice 无效")
			}
			fc["mode"] = "ANY"
			fc["allowedFunctionNames"] = []string{chosen.Function.Name}
		}
		out["toolConfig"] = map[string]any{"functionCallingConfig": fc}
	}
	if v := in["response_format"]; len(v) > 0 && string(v) != "null" {
		var format struct {
			Type   string `json:"type"`
			Schema struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"json_schema"`
		}
		if json.Unmarshal(v, &format) != nil {
			return nil, providerhttp.Invalid("Google native", "response_format 无效")
		}
		switch format.Type {
		case "text":
		case "json_object":
			config["responseMimeType"] = json.RawMessage(`"application/json"`)
		case "json_schema":
			if !json.Valid(format.Schema.Schema) {
				return nil, providerhttp.Invalid("Google native", "JSON schema 缺失")
			}
			config["responseMimeType"] = json.RawMessage(`"application/json"`)
			config["responseJsonSchema"] = format.Schema.Schema
		default:
			return nil, providerhttp.Invalid("Google native", "response_format 不支持")
		}
	}
	if v := in["reasoning_effort"]; len(v) > 0 && string(v) != "null" {
		var effort string
		if json.Unmarshal(v, &effort) != nil {
			return nil, providerhttp.Invalid("Google native", "reasoning_effort 无效")
		}
		switch effort {
		case "low", "medium", "high", "minimal":
			var thinking any = map[string]string{"thinkingLevel": effort}
			if strings.HasPrefix(model, "gemini-2.5-") {
				budgets := map[string]int{"minimal": 1024, "low": 1024, "medium": 8192, "high": 24576}
				thinking = map[string]int{"thinkingBudget": budgets[effort]}
			}
			b, _ := json.Marshal(thinking)
			config["thinkingConfig"] = b
		default:
			return nil, providerhttp.Invalid("Google native", "CLI 暂不支持该 reasoning_effort")
		}
	}
	if len(config) > 0 {
		out["generationConfig"] = config
	}
	return json.Marshal(out)
}
