package codearts

import (
	"encoding/json"
	"net/http"
)

// validateChatReply checks the non-streaming OpenAI Chat envelope, not its
// content. A message may contain tools, refusal, reasoning, future fields or
// empty/truncated output. The empty struct skips all message fields without
// decoding/copying large text or tool arguments; callers keep the original body.
func validateChatReply(raw []byte) error {
	var envelope struct {
		Choices []struct {
			Message *struct{} `json:"message"`
		} `json:"choices"`
		ErrorCode string           `json:"error_code"`
		ErrorMsg  string           `json:"error_msg"`
		Error     *json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return &UpstreamError{Status: http.StatusBadGateway, Message: "上游非流式响应不是有效的对话 JSON"}
	}
	if msg, status, bad := classifyBusinessErrorFields(envelope.ErrorCode, envelope.ErrorMsg); bad {
		return UpstreamBusinessError(status, msg)
	}
	// A 200 error envelope has no reliable transport status. Treat it as an
	// upstream protocol failure without guessing credential faults from text.
	if envelope.Error != nil {
		return &UpstreamError{Status: http.StatusBadGateway, Message: "上游非流式响应包含错误信封"}
	}
	if len(envelope.Choices) == 0 {
		return &UpstreamError{Status: http.StatusBadGateway, Message: "上游非流式响应缺少有效 choices"}
	}
	for _, choice := range envelope.Choices {
		if choice.Message == nil {
			return &UpstreamError{Status: http.StatusBadGateway, Message: "上游非流式响应缺少 message 对象"}
		}
	}
	return nil
}
