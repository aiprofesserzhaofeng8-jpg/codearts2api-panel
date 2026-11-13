// Package protocol 负责 Anthropic Messages API 与 OpenAI Chat Completions
// 之间的**纯数据转换**。
//
// 这里不碰 HTTP、不写响应：handler 与 SSE 流式编码留在 internal/server。
// 这样拆分的好处是转换逻辑可以脱离网络被完整单测（见 anthropic_test.go），
// 同时 server 不再是一个既做传输、又做协议适配的上帝包。
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	apicompat "codearts2api/internal/responsescompat"
)

// Request 是 Anthropic Messages 请求体中网关关心的字段。
type Request struct {
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	System        json.RawMessage `json:"system"`
	Messages      []Message       `json:"messages"`
	Stream        bool            `json:"stream"`
	Temperature   *float64        `json:"temperature"`
	TopP          *float64        `json:"top_p"`
	StopSequences []string        `json:"stop_sequences"`
	Tools         []Tool          `json:"tools"`
	ToolChoice    json.RawMessage `json:"tool_choice"`
	Thinking      json.RawMessage `json:"thinking"`
	OutputConfig  json.RawMessage `json:"output_config"`
}

// Message 是一条 Anthropic 消息（content 为字符串或内容块数组）。
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// Tool 是 Anthropic 的工具声明。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Reply 是转换后的 Anthropic message 及其文本统计。
//
// Text / Thinking 供调用方统计输出长度（写日志用），Payload 是最终响应体。
type Reply struct {
	Payload  map[string]any
	Text     string
	Thinking string
}

// --- Anthropic → OpenAI ---

// ToOpenAI 把 Anthropic 请求转换为 OpenAI Chat Completions 请求体。
func ToOpenAI(req *Request, model string) (map[string]any, error) { return toOpenAI(req, model, false) }

// ToOpenAINativeMessages preserves Anthropic thinking controls and signed history.
// The HTTP boundary selects this only after resolving a native Messages route.
func ToOpenAINativeMessages(req *Request, model string) (map[string]any, error) {
	return toOpenAI(req, model, true)
}

// RequiresNativeMessages identifies controls/history that need a native
// Messages target. It does not infer capabilities from model names.
func RequiresNativeMessages(req *Request) bool {
	var thinking map[string]json.RawMessage
	if json.Unmarshal(req.Thinking, &thinking) == nil {
		var typ string
		_ = json.Unmarshal(thinking["type"], &typ)
		if typ == "enabled" || typ == "adaptive" {
			return true
		}
	}
	if len(req.OutputConfig) > 0 && string(bytes.TrimSpace(req.OutputConfig)) != "null" {
		return true
	}
	for _, m := range req.Messages {
		var blocks []struct {
			Type      string          `json:"type"`
			Signature json.RawMessage `json:"signature"`
		}
		if json.Unmarshal(m.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "redacted_thinking" || (b.Type == "thinking" && hasThinkingSignature(b.Signature)) {
					return true
				}
			}
		}
	}
	return false
}

// Empty signatures emitted by compatible clients carry no native signature.
// Non-string values remain native-only so malformed opaque data is not stripped.
func hasThinkingSignature(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var signature string
	return json.Unmarshal(raw, &signature) != nil || signature != ""
}
func toOpenAI(req *Request, model string, native bool) (map[string]any, error) {
	out := map[string]any{
		"model":  model,
		"stream": req.Stream,
	}
	if native {
		if len(req.Thinking) > 0 {
			out["thinking"] = req.Thinking
		}
		if len(req.OutputConfig) > 0 {
			out["output_config"] = req.OutputConfig
		}
	} else if err := applyAnthropicThinking(req, out); err != nil {
		return nil, err
	}
	if req.MaxTokens > 0 {
		out["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		out["stop"] = req.StopSequences
	}

	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if err := validateSystemContent(req.System); err != nil {
		return nil, err
	}
	if sys := SystemText(req.System); sys != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": sys})
	}
	for _, m := range req.Messages {
		converted, err := messageToOpenAI(m, native)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, converted...)
	}
	out["messages"] = contiguousToolResults(msgs)

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			fn := map[string]any{"name": t.Name}
			if t.Description != "" {
				fn["description"] = t.Description
			}
			if len(t.InputSchema) > 0 {
				fn["parameters"] = json.RawMessage(t.InputSchema)
			} else {
				fn["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{"type": "function", "function": fn})
		}
		out["tools"] = tools
		if len(req.ToolChoice) > 0 {
			out["tool_choice"] = ToolChoice(req.ToolChoice)
		}
	}
	return out, nil
}

// ToolChoice 把 Anthropic 的 tool_choice 映射为 OpenAI 形态。
func ToolChoice(raw json.RawMessage) any {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "auto"
	}
	switch obj["type"] {
	case "any":
		return "required"
	case "tool":
		if name, ok := obj["name"].(string); ok {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}
		}
		return "auto"
	case "none":
		return "none"
	default:
		return "auto"
	}
}

// MessageToOpenAI 把一条 Anthropic 消息展开为一条或多条 OpenAI 消息。
//
// tool_use 会变成 assistant.tool_calls，tool_result 会变成独立的 role=tool 消息。
func MessageToOpenAI(m Message) ([]map[string]any, error) { return messageToOpenAI(m, false) }
func messageToOpenAI(m Message, native bool) ([]map[string]any, error) {
	if m.Role != "user" && m.Role != "assistant" {
		return nil, fmt.Errorf("Messages 消息角色须为 user 或 assistant")
	}
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		return []map[string]any{{"role": m.Role, "content": text}}, nil
	}
	var blocks []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(m.Content))
	decoder.UseNumber()
	if err := decoder.Decode(&blocks); err != nil {
		return nil, fmt.Errorf("content 字段格式不支持")
	}
	var ordinary []map[string]any
	var thought []any
	var reasoning strings.Builder
	hasReasoning := false
	var assistantParts []any
	toolResults := []string{}
	for _, b := range blocks {
		switch b["type"] {
		case "thinking", "redacted_thinking":
			if m.Role != "assistant" {
				return nil, fmt.Errorf("思考块只允许在助手消息中")
			}
			signature, _ := json.Marshal(b["signature"])
			if (b["type"] == "redacted_thinking" || hasThinkingSignature(signature)) && !native {
				return nil, fmt.Errorf("签名或加密思考历史需要兼容的原生 Messages 来源，未删除历史")
			}
			if native {
				thought = append(thought, b)
			} else {
				t, ok := b["thinking"].(string)
				if !ok {
					return nil, fmt.Errorf("thinking 内容须为字符串")
				}
				reasoning.WriteString(t)
				hasReasoning = true
			}
			continue
		case "text":
			t, ok := b["text"].(string)
			if !ok {
				return nil, fmt.Errorf("text 内容须为字符串")
			}
			if t != "" {
				assistantParts = append(assistantParts, map[string]any{"type": "text", "text": t})
			}
		case "image":
			u := ImageURL(b)
			if u == "" {
				return nil, fmt.Errorf("image 内容缺少有效的图片 source，未丢弃图片")
			}
			assistantParts = append(assistantParts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": u}})
		case "tool_use":
			if m.Role != "assistant" {
				return nil, fmt.Errorf("tool_use 只允许在助手消息中")
			}
		case "tool_result":
			if m.Role != "user" {
				return nil, fmt.Errorf("tool_result 只允许在用户消息中")
			}
			t, _, err := toolResultContent(b)
			if err != nil {
				return nil, err
			}
			toolResults = append(toolResults, t)
		default:
			return nil, fmt.Errorf("当前 Messages 转换不支持内容块 %q，未静默删除", Str(b["type"]))
		}
		ordinary = append(ordinary, b)
	}
	raw := m.Content
	if len(thought) > 0 {
		var err error
		raw, err = json.Marshal(ordinary)
		if err != nil {
			return nil, err
		}
	}
	converted, err := apicompat.AnthropicMessageToChatMessages(apicompat.AnthropicMessage{Role: m.Role, Content: raw})
	if err != nil {
		return nil, err
	}
	// The shared bridge owns tool calls, result expansion and image placement.
	// Local contracts retain ordinary reasoning and native opaque history without
	// applying Sub2API's provider-specific history filters or system cleanup.
	out := make([]map[string]any, 0, len(converted))
	resultIndex := 0
	for _, c := range converted {
		msg := map[string]any{"role": c.Role, "content": ""}
		if len(c.Content) > 0 {
			var content any
			decoder = json.NewDecoder(bytes.NewReader(c.Content))
			decoder.UseNumber()
			if err := decoder.Decode(&content); err != nil {
				return nil, err
			}
			msg["content"] = content
		}
		if len(c.ToolCalls) > 0 {
			calls := make([]any, 0, len(c.ToolCalls))
			for _, call := range c.ToolCalls {
				calls = append(calls, map[string]any{"id": call.ID, "type": call.Type, "function": map[string]any{"name": call.Function.Name, "arguments": call.Function.Arguments}})
			}
			msg["tool_calls"] = calls
		}
		if c.Role == "tool" {
			msg["tool_call_id"] = c.ToolCallID
			if resultIndex < len(toolResults) {
				msg["content"] = toolResults[resultIndex]
				resultIndex++
			}
		}
		out = append(out, msg)
	}
	if m.Role == "assistant" {
		if len(out) == 0 {
			out = []map[string]any{{"role": "assistant"}}
		}
		msg := out[0]
		msg["content"] = ""
		if len(assistantParts) == 1 && assistantParts[0].(map[string]any)["type"] == "text" {
			msg["content"] = assistantParts[0].(map[string]any)["text"]
		} else if len(assistantParts) > 0 {
			msg["content"] = assistantParts
		}
		if hasReasoning {
			msg["reasoning_content"] = reasoning.String()
		}
		if len(thought) > 0 {
			msg["anthropic_thinking_blocks"] = thought
		}
	}
	return out, nil
}

func validateSystemContent(raw json.RawMessage) error {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return fmt.Errorf("system 须为字符串或文本块数组")
	}
	for _, b := range blocks {
		var typ string
		if json.Unmarshal(b["type"], &typ) != nil || typ != "text" || json.Unmarshal(b["text"], &text) != nil {
			return fmt.Errorf("system 仅支持文本块，未静默删除其他内容")
		}
	}
	return nil
}

// Parallel tool results may span several consecutive user messages. Keep the
// outputs contiguous before lifting associated media or user text after them.
func contiguousToolResults(messages []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for i := 0; i < len(messages); {
		if messages[i]["role"] != "tool" {
			out = append(out, messages[i])
			i++
			continue
		}
		var outputs, following []map[string]any
		for i < len(messages) && (messages[i]["role"] == "tool" || messages[i]["role"] == "user") {
			if messages[i]["role"] == "tool" {
				outputs = append(outputs, messages[i])
			} else {
				following = append(following, messages[i])
			}
			i++
		}
		out = append(out, outputs...)
		out = append(out, following...)
	}
	return out
}

func toolResultContent(block map[string]any) (string, []any, error) {
	content, ok := block["content"].([]any)
	if !ok {
		return ToolResultText(block), nil, nil
	}
	textBlocks := make([]any, 0, len(content))
	var images []any
	for _, item := range content {
		part, ok := item.(map[string]any)
		if !ok {
			textBlocks = append(textBlocks, item)
			continue
		}
		switch part["type"] {
		case "image":
			url := ImageURL(part)
			if url == "" {
				return "", nil, fmt.Errorf("工具结果图片缺少有效的 source，未丢弃图片")
			}
			images = append(images, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
		case "document", "audio", "video", "file":
			return "", nil, fmt.Errorf("当前 Messages 转换尚不支持工具结果中的 %s 内容", Str(part["type"]))
		default:
			textBlocks = append(textBlocks, item)
		}
	}
	return ToolResultText(map[string]any{"content": textBlocks}), images, nil
}

// SystemText 提取 system 字段（字符串或文本块数组）。
func SystemText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []map[string]any
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b["type"] == "text" {
			if t, ok := b["text"].(string); ok && t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// ImageURL 把 Anthropic 图片块转成 data: / http(s) 地址。
func ImageURL(block map[string]any) string {
	src, ok := block["source"].(map[string]any)
	if !ok {
		return ""
	}
	switch src["type"] {
	case "base64":
		media := Str(src["media_type"])
		data := Str(src["data"])
		if data == "" {
			return ""
		}
		if media == "" {
			media = "image/png"
		}
		return "data:" + media + ";base64," + data
	case "url":
		return Str(src["url"])
	}
	return ""
}

// ToolResultText 提取工具结果中的文本。
func ToolResultText(block map[string]any) string {
	switch c := block["content"].(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				} else {
					if b, err := json.Marshal(m); err == nil {
						parts = append(parts, string(b))
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	if block["content"] != nil {
		if b, err := json.Marshal(block["content"]); err == nil {
			return string(b)
		}
	}
	return ""
}

// --- OpenAI → Anthropic ---

// FromOpenAI 把一次非流式 OpenAI 响应映射为 Anthropic message。
func FromOpenAI(raw []byte, model string) Reply {
	var resp struct {
		Choices []struct {
			Message struct {
				Content            string            `json:"content"`
				ReasoningContent   string            `json:"reasoning_content"`
				ReasoningSignature string            `json:"reasoning_signature"`
				NativeThinking     []json.RawMessage `json:"anthropic_thinking_blocks"`
				Refusal            string            `json:"refusal"`
				ToolCalls          []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     *int `json:"prompt_tokens"`
			CompletionTokens *int `json:"completion_tokens"`
			CacheHit         *int `json:"prompt_cache_hit_tokens"`
			PromptDetails    struct {
				Cached   *int `json:"cached_tokens"`
				Creation *int `json:"cache_creation_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.Choices) == 0 {
		return Reply{}
	}

	content := make([]map[string]any, 0, 3)
	stopReason := "end_turn"
	var text, thinking string
	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]
		thinking = ch.Message.ReasoningContent
		text = ch.Message.Content
		if len(ch.Message.NativeThinking) > 0 {
			for _, raw := range ch.Message.NativeThinking {
				var block map[string]any
				if json.Unmarshal(raw, &block) == nil {
					content = append(content, block)
				} else {
					return Reply{}
				}
			}
		} else if thinking != "" {
			block := map[string]any{"type": "thinking", "thinking": thinking}
			if ch.Message.ReasoningSignature != "" {
				block["signature"] = ch.Message.ReasoningSignature
			}
			content = append(content, block)
		}
		if text != "" {
			content = append(content, map[string]any{"type": "text", "text": text})
		}
		if ch.Message.Refusal != "" {
			content = append(content, map[string]any{"type": "text", "text": ch.Message.Refusal})
			text += ch.Message.Refusal
		}
		for _, tc := range ch.Message.ToolCalls {
			input := json.RawMessage(tc.Function.Arguments)
			if !json.Valid(input) || tc.Function.Name == "" {
				return Reply{}
			}
			id := tc.ID
			if id == "" {
				id = "toolu_" + randomHex(12)
			}
			content = append(content, map[string]any{
				"type": "tool_use", "id": id, "name": tc.Function.Name, "input": input,
			})
			stopReason = "tool_use"
		}
		if ch.FinishReason == "length" {
			stopReason = "max_tokens"
		}
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": ""})
	}

	in := 0
	if resp.Usage.PromptTokens != nil {
		in = max(0, *resp.Usage.PromptTokens)
	} else {
		in = EstimatePromptLen(raw)
	}
	out := 0
	if resp.Usage.CompletionTokens != nil {
		out = max(0, *resp.Usage.CompletionTokens)
	} else {
		out = len([]rune(text)) + len([]rune(thinking))
	}
	usage := map[string]any{"input_tokens": in, "output_tokens": out}
	// OpenAI includes cache hits in prompt_tokens; Messages reports them
	// separately. Match the gateway's existing cache-alias precedence.
	cached := 0
	if resp.Usage.PromptDetails.Cached != nil {
		cached = *resp.Usage.PromptDetails.Cached
	}
	if cached == 0 && resp.Usage.CacheHit != nil {
		cached = *resp.Usage.CacheHit
	}
	if resp.Usage.PromptDetails.Cached != nil || resp.Usage.CacheHit != nil {
		cached = min(max(0, cached), in)
		usage["input_tokens"] = in - cached
		usage["cache_read_input_tokens"] = cached
	}
	if resp.Usage.PromptDetails.Creation != nil {
		creation := min(max(0, *resp.Usage.PromptDetails.Creation), in-cached)
		usage["input_tokens"] = in - cached - creation
		usage["cache_creation_input_tokens"] = creation
	}

	return Reply{
		Text:     text,
		Thinking: thinking,
		Payload: map[string]any{
			"id":            "msg_" + randomHex(12),
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       content,
			"stop_reason":   stopReason,
			"stop_sequence": nil,
			"usage":         usage,
		},
	}
}

// Str 从 map 中取字符串。
func Str(v any) string {
	s, _ := v.(string)
	return s
}
