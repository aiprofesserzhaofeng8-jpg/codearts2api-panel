// Derived from Wei-Shaw/sub2api at 6b4b37db43443b5e88e424fe3c69602cc3810dfb.
// Copyright Wei-Shaw and Sub2API contributors. Licensed under LGPL-3.0;
// see LICENSE, COPYING, and UPSTREAM.md for source and local adaptation notes.
package apicompat

import (
	"encoding/json"
	"strings"
)

// AnthropicMessageToChatMessages is the direct Messages-to-Chat message bridge.
// Callers validate supported content and retain native signature semantics.
func AnthropicMessageToChatMessages(m AnthropicMessage) ([]ChatMessage, error) {
	return anthropicMsgToChatMessages(m)
}

func anthropicMsgToChatMessages(m AnthropicMessage) ([]ChatMessage, error) {
	switch m.Role {
	case "assistant":
		return anthropicAssistantToChatMessages(m.Content)
	default: // "user" and any unknown role
		return anthropicUserToChatMessages(m.Content)
	}
}

func anthropicUserToChatMessages(raw json.RawMessage) ([]ChatMessage, error) {
	// Plain string → single user message.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		content, _ := json.Marshal(s)
		return []ChatMessage{{Role: "user", Content: content}}, nil
	}

	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	var out []ChatMessage
	var toolResultImageParts []ChatContentPart

	// tool_result → "tool" role messages, text extracted; images deferred.
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		text, imageParts := convertToolResultOutput(b)
		content, _ := json.Marshal(text)
		out = append(out, ChatMessage{
			Role:       "tool",
			Content:    content,
			ToolCallID: b.ToolUseID,
		})
		if len(imageParts) > 0 {
			toolResultImageParts = append(toolResultImageParts, ChatContentPart{Type: "text", Text: "Images returned by tool " + b.ToolUseID + ":"})
		}
		for _, ip := range imageParts {
			toolResultImageParts = append(toolResultImageParts, ChatContentPart{
				Type:     "image_url",
				ImageURL: &ChatImageURL{URL: ip.ImageURL},
			})
		}
	}

	// Remaining text + image blocks → user message. The double-conversion path
	// (responsesContentPartsToChatContent) folds text-only content into a single
	// string joined with "\n\n" and only uses the parts-array form when an image
	// is present — strict chat upstreams reject array content — so the direct
	// bridge preserves that folding.
	var textParts []string
	var parts []ChatContentPart
	hasImage := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				textParts = append(textParts, b.Text)
				parts = append(parts, ChatContentPart{Type: "text", Text: b.Text})
			}
		case "image":
			if uri := anthropicImageToDataURI(b.Source); uri != "" {
				hasImage = true
				parts = append(parts, ChatContentPart{
					Type:     "image_url",
					ImageURL: &ChatImageURL{URL: uri},
				})
			}
		}
	}
	if len(toolResultImageParts) > 0 {
		hasImage = true
		parts = append(parts, toolResultImageParts...)
	}

	if !hasImage {
		if len(textParts) > 0 {
			content, _ := json.Marshal(strings.Join(textParts, "\n\n"))
			out = append(out, ChatMessage{Role: "user", Content: content})
		}
		return out, nil
	}

	content, err := json.Marshal(parts)
	if err != nil {
		return nil, err
	}
	out = append(out, ChatMessage{Role: "user", Content: content})

	return out, nil
}

func anthropicAssistantToChatMessages(raw json.RawMessage) ([]ChatMessage, error) {
	// Plain string → single assistant message.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		content, _ := json.Marshal(s)
		return []ChatMessage{{Role: "assistant", Content: content}}, nil
	}

	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	msg := ChatMessage{Role: "assistant"}
	text := extractAnthropicTextFromBlocks(blocks)
	if text != "" {
		content, _ := json.Marshal(text)
		msg.Content = content
	}

	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		args := "{}"
		if len(b.Input) > 0 {
			args = string(b.Input)
		}
		msg.ToolCalls = append(msg.ToolCalls, ChatToolCall{
			ID:   b.ID,
			Type: "function",
			Function: ChatFunctionCall{
				Name:      b.Name,
				Arguments: args,
			},
		})
	}

	msg.ReasoningContent = anthropicThinkingToReasoningContent(blocks, len(msg.ToolCalls) > 0)

	return []ChatMessage{msg}, nil
}

func anthropicThinkingToReasoningContent(blocks []AnthropicContentBlock, hasToolCalls bool) string {
	if !hasToolCalls {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "thinking" && b.Thinking != "" {
			parts = append(parts, b.Thinking)
		}
	}
	return strings.Join(parts, "\n")
}

func anthropicImageToDataURI(src *AnthropicImageSource) string {
	if src == nil {
		return ""
	}
	if src.Type == "url" {
		return src.URL
	}
	if src.Data == "" {
		return ""
	}
	mediaType := src.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	return "data:" + mediaType + ";base64," + src.Data
}

func convertToolResultOutput(b AnthropicContentBlock) (string, []ResponsesContentPart) {
	if len(b.Content) == 0 {
		return "", nil
	}

	// Try plain string content.
	var s string
	if err := json.Unmarshal(b.Content, &s); err == nil {
		return s, nil
	}

	// Array of content blocks — may contain text and/or images.
	var inner []AnthropicContentBlock
	if err := json.Unmarshal(b.Content, &inner); err != nil {
		return "", nil
	}

	// Separate text (for function_call_output) from images (for user message).
	var textParts []string
	var imageParts []ResponsesContentPart
	for _, ib := range inner {
		switch ib.Type {
		case "text":
			if ib.Text != "" {
				textParts = append(textParts, ib.Text)
			}
		case "image":
			if uri := anthropicImageToDataURI(ib.Source); uri != "" {
				imageParts = append(imageParts, ResponsesContentPart{Type: "input_image", ImageURL: uri})
			}
		}
	}

	text := strings.Join(textParts, "\n")
	return text, imageParts
}

func extractAnthropicTextFromBlocks(blocks []AnthropicContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}
