package kiro

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type messageContext struct {
	Tools   []any `json:"tools,omitempty"`
	Results []any `json:"toolResults,omitempty"`
}
type userMessage struct {
	Content string         `json:"content"`
	Model   string         `json:"modelId"`
	Origin  string         `json:"origin"`
	Context messageContext `json:"userInputMessageContext"`
	Images  []any          `json:"images,omitempty"`
}
type assistantMessage struct {
	Content string `json:"content"`
	Tools   []any  `json:"toolUses,omitempty"`
}

func contentText(raw json.RawMessage) (string, []any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil, nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return "", nil, invalid("消息 content 必须为文本或内容块数组")
	}
	var parts []string
	var images []any
	for _, b := range blocks {
		switch field(b, "type") {
		case "text":
			parts = append(parts, field(b, "text"))
		case "image_url":
			var v struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(b["image_url"], &v) != nil {
				return "", nil, invalid("图片格式无效")
			}
			prefix, data, ok := strings.Cut(v.URL, ",")
			if !ok || !strings.HasPrefix(prefix, "data:image/") || !strings.HasSuffix(prefix, ";base64") {
				return "", nil, invalid("图片仅支持 base64 data URL，不读取外部地址")
			}
			format := strings.TrimSuffix(strings.TrimPrefix(prefix, "data:image/"), ";base64")
			if format == "jpg" {
				format = "jpeg"
			}
			if format != "png" && format != "jpeg" && format != "webp" && format != "gif" {
				return "", nil, invalid("不支持该图片类型")
			}
			if len(data) > 6<<20 {
				return "", nil, invalid("图片过大")
			}
			decoded, e := base64.StdEncoding.DecodeString(data)
			if e != nil || len(decoded) == 0 || len(decoded) > 4<<20 {
				return "", nil, invalid("图片 base64 无效或过大")
			}
			images = append(images, map[string]any{"format": format, "source": map[string]string{"bytes": data}})
		default:
			return "", nil, invalid("暂不支持该消息内容块")
		}
	}
	return strings.Join(parts, "\n"), images, nil
}
func chatBody(a Credentials, r provider.Request) (any, error) {
	if r.Model == "" || len(r.Model) > 200 || strings.ContainsAny(r.Model, "/\r\n\x00") {
		return nil, invalid("型号无效")
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(r.Body, &input) != nil || input == nil {
		return nil, invalid("请求必须为 JSON 对象")
	}
	for _, key := range []string{"response_format", "thinking", "reasoning_effort"} {
		if b := input[key]; len(b) > 0 && string(b) != "null" {
			return nil, invalid("暂不支持 " + key + " 控制，请移除此参数")
		}
	}
	if b := input["parallel_tool_calls"]; len(b) > 0 && string(b) != "null" {
		var parallel bool
		if json.Unmarshal(b, &parallel) != nil || !parallel {
			return nil, invalid("原生端点不支持禁止并行工具调用")
		}
	}
	for _, key := range []string{"temperature", "top_p", "seed", "stop", "frequency_penalty", "presence_penalty"} {
		if b := input[key]; len(b) > 0 && string(b) != "null" {
			return nil, invalid("原生端点不支持 " + key + " 控制")
		}
	}
	var n int
	if b := input["n"]; len(b) > 0 && (json.Unmarshal(b, &n) != nil || n != 1) {
		return nil, invalid("仅支持 n=1")
	}
	// Kiro has no native max_tokens field. The upstream controls the output limit;
	// accept the mandatory Anthropic compatibility parameter, without claiming it
	// is enforced. See docs/KIRO.md for this protocol limitation.
	var tools []map[string]json.RawMessage
	if b := input["tools"]; len(b) > 0 && string(b) != "null" && json.Unmarshal(b, &tools) != nil {
		return nil, invalid("tools 格式无效")
	}
	choice := field(input, "tool_choice")
	if b := input["tool_choice"]; len(b) > 0 && string(b) != "null" && choice != "auto" && choice != "none" {
		return nil, invalid("tool_choice 仅支持 auto / none")
	}
	if choice == "none" {
		tools = nil
	}
	var specs []any
	for _, tool := range tools {
		if field(tool, "type") != "function" {
			return nil, invalid("仅支持 function 工具")
		}
		var f map[string]json.RawMessage
		if json.Unmarshal(tool["function"], &f) != nil || field(f, "name") == "" {
			return nil, invalid("工具定义无效")
		}
		schema := f["parameters"]
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(schema, &obj) != nil || obj == nil {
			return nil, invalid("工具 parameters 必须为 JSON 对象")
		}
		desc := field(f, "description")
		if len(desc) > 10000 {
			return nil, invalid("工具描述过长，请缩短到 10000 字节以内")
		}
		specs = append(specs, map[string]any{"toolSpecification": map[string]any{"name": field(f, "name"), "description": desc, "inputSchema": map[string]any{"json": schema}}})
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(input["messages"], &messages) != nil || len(messages) == 0 {
		return nil, invalid("缺少 messages")
	}
	var systems []string
	var history []any
	var current *userMessage
	var lastAssistant *assistantMessage
	flushUser := func() {
		if current != nil {
			history = append(history, map[string]any{"userInputMessage": *current})
			current = nil
		}
	}
	flushAssistant := func() {
		if lastAssistant != nil {
			history = append(history, map[string]any{"assistantResponseMessage": *lastAssistant})
			lastAssistant = nil
		}
	}
	for _, m := range messages {
		role := field(m, "role")
		text, images, e := contentText(m["content"])
		if e != nil {
			return nil, e
		}
		switch role {
		case "system", "developer":
			if len(images) > 0 {
				return nil, invalid("系统提示词不支持图片")
			}
			systems = append(systems, "["+role+"]\n"+text+"\n[/"+role+"]")
		case "user", "tool":
			flushAssistant()
			if current == nil {
				current = &userMessage{Model: r.Model, Origin: "AI_EDITOR"}
			}
			if role == "tool" {
				id := field(m, "tool_call_id")
				if id == "" || len(images) > 0 {
					return nil, invalid("工具结果需要 tool_call_id 且只支持文本")
				}
				current.Context.Results = append(current.Context.Results, map[string]any{"toolUseId": id, "content": []any{map[string]string{"text": text}}, "status": "success"})
			} else {
				if current.Content != "" {
					current.Content += "\n"
				}
				current.Content += text
				current.Images = append(current.Images, images...)
			}
		case "assistant":
			if len(images) > 0 {
				return nil, invalid("助手历史不支持图片")
			}
			flushUser()
			if lastAssistant == nil {
				lastAssistant = &assistantMessage{}
			}
			if lastAssistant.Content != "" {
				lastAssistant.Content += "\n"
			}
			lastAssistant.Content += text
			if refusal := field(m, "refusal"); refusal != "" {
				lastAssistant.Content += refusal
			}
			if reason := field(m, "reasoning_content"); reason != "" {
				lastAssistant.Content = "<thinking>" + reason + "</thinking>\n" + lastAssistant.Content
			}
			var calls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			}
			if b := m["tool_calls"]; len(b) > 0 && string(b) != "null" && json.Unmarshal(b, &calls) != nil {
				return nil, invalid("历史工具调用无效")
			}
			for _, call := range calls {
				var obj map[string]json.RawMessage
				if call.ID == "" || call.Type != "function" || call.Function.Name == "" || json.Unmarshal([]byte(call.Function.Arguments), &obj) != nil || obj == nil {
					return nil, invalid("历史工具参数必须为完整 JSON 对象")
				}
				lastAssistant.Tools = append(lastAssistant.Tools, map[string]any{"toolUseId": call.ID, "name": call.Function.Name, "input": json.RawMessage(call.Function.Arguments)})
			}
		default:
			return nil, invalid("未知消息角色")
		}
	}
	if lastAssistant != nil || current == nil {
		return nil, invalid("最后一条消息必须是用户输入或工具结果")
	}
	current.Context.Tools = specs
	if len(systems) > 0 {
		prefix := strings.Join(systems, "\n\n") + "\n\n"
		if len(history) > 0 {
			if v, ok := history[0].(map[string]any)["userInputMessage"].(userMessage); ok {
				v.Content = prefix + v.Content
				history[0] = map[string]any{"userInputMessage": v}
			} else {
				return nil, invalid("对话历史应以用户消息开始")
			}
		} else {
			current.Content = prefix + current.Content
		}
	}
	id, e := uuid()
	if e != nil {
		return nil, e
	}
	session := r.SessionID
	if session == "" {
		session = id
	}
	state := map[string]any{"conversationId": session, "agentContinuationId": id, "agentTaskType": "vibe", "chatTriggerType": "MANUAL", "currentMessage": map[string]any{"userInputMessage": current}}
	if len(history) > 0 {
		state["history"] = history
	}
	body := map[string]any{"conversationState": state}
	if a.ProfileARN != "" {
		body["profileArn"] = a.ProfileARN
	}
	return body, nil
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	a, e := decode(raw)
	if e != nil {
		return e
	}
	region, ok := c.Regions[a.Region]
	if !ok {
		return invalid("region 无效")
	}
	body, e := chatBody(a, r)
	if e != nil {
		return e
	}
	endpoint, target := region.Runtime+"/generateAssistantResponse", ""
	if a.APIKey != "" {
		endpoint = region.Runtime + "/"
		target = "AmazonCodeWhispererStreamingService.GenerateAssistantResponse"
	}
	resp, e := c.request(ctx, http.MethodPost, endpoint, a, target, body)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("Kiro", resp)
	}
	return convertResponse(ctx, resp.Body, provider.ModelID(provider.Kiro, a.Region, r.Model), emit)
}
