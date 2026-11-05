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
			Type string `json:"type"`
		}
		if json.Unmarshal(m.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "thinking" || b.Type == "redacted_thinking" {
					return true
				}
			}
		}
	}
	return false
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
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return []map[string]any{{"role": m.Role, "content": text}}, nil
	}

	var blocks []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(m.Content))
	decoder.UseNumber()
	if err := decoder.Decode(&blocks); err != nil {
		return nil, fmt.Errorf("content 字段格式不支持")
	}

	role := m.Role
	var (
		parts          []any
		toolCalls      []any
		toolMsgs       []map[string]any
		thinkingBlocks []any
		toolImages     []any
	)

	for _, b := range blocks {
		switch b["type"] {
		case "text":
			t, ok := b["text"].(string)
			if !ok {
				return nil, fmt.Errorf("text 内容须为字符串")
			}
			if t != "" {
				parts = append(parts, map[string]any{"type": "text", "text": t})
			}
		case "image":
			if u := ImageURL(b); u != "" {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": u}})
			} else {
				return nil, fmt.Errorf("image 内容缺少有效的图片 source，未丢弃图片")
			}
		case "document", "audio", "video", "input_audio", "file":
			return nil, fmt.Errorf("当前 Messages 转换尚不支持 %s 内容，请转换为文本或图片后提交", Str(b["type"]))
		case "tool_use":
			args, _ := json.Marshal(b["input"])
			toolCalls = append(toolCalls, map[string]any{
				"id":   Str(b["id"]),
				"type": "function",
				"function": map[string]any{
					"name":      Str(b["name"]),
					"arguments": string(args),
				},
			})
		case "tool_result":
			text, images, err := toolResultContent(b)
			if err != nil {
				return nil, err
			}
			if len(images) > 0 {
				toolImages = append(toolImages, map[string]any{"type": "text", "text": "Images returned by tool " + Str(b["tool_use_id"]) + ":"})
				toolImages = append(toolImages, images...)
			}
			toolMsgs = append(toolMsgs, map[string]any{
				"role":         "tool",
				"tool_call_id": Str(b["tool_use_id"]),
				"content":      text,
			})
		case "thinking", "redacted_thinking":
			if native {
				if m.Role != "assistant" {
					return nil, fmt.Errorf("思考块只允许在助手消息中")
				}
				thinkingBlocks = append(thinkingBlocks, b)
			}
		default:
			return nil, fmt.Errorf("当前 Messages 转换不支持内容块 %q，未静默删除", Str(b["type"]))
		}
	}

	out := make([]map[string]any, 0, 2)
	msg := map[string]any{"role": role}
	if len(thinkingBlocks) > 0 {
		msg["anthropic_thinking_blocks"] = thinkingBlocks
	}
	if len(parts) == 1 {
		// 单个文本块直接摊平成字符串，与 OpenAI 的常见写法一致
		if p, ok := parts[0].(map[string]any); ok && p["type"] == "text" {
			msg["content"] = p["text"]
		} else {
			msg["content"] = parts
		}
	} else if len(parts) > 1 {
		msg["content"] = parts
	} else {
		msg["content"] = ""
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	if role == "user" && len(toolMsgs) > 0 {
		out = append(out, toolMsgs...)
		toolMsgs = nil
	}
	if len(parts) > 0 || len(toolCalls) > 0 || len(thinkingBlocks) > 0 {
		out = append(out, msg)
	}
	out = append(out, toolMsgs...)
	// Chat Completions tool messages accept text, not image blocks. Complete
	// the tool-result group first, then preserve images as a user media message.
	if len(toolImages) > 0 {
		out = append(out, map[string]any{"role": "user", "content": toolImages})
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
