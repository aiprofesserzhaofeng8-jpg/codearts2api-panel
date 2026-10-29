package codearts

import (
	"codearts2api/internal/requestpolicy"
	"encoding/json"
	"strings"
)

// Generic permission, device and quota errors must never trigger a rewrite.
func (e *UpstreamError) PromptBlocked() bool {
	var obj struct {
		Code      string `json:"code"`
		ErrorCode string `json:"error_code"`
		Message   string `json:"message"`
		ErrorMsg  string `json:"error_msg"`
		Error     struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(e.Message), &obj) == nil {
		return requestpolicy.ExplicitContentBlock(e.Status, obj.Error.Code, obj.Error.Message) || requestpolicy.ExplicitContentBlock(e.Status, obj.Error.Type, obj.Error.Message) || requestpolicy.ExplicitContentBlock(e.Status, obj.Code, obj.Message+" "+obj.ErrorMsg) || requestpolicy.ExplicitContentBlock(e.Status, obj.ErrorCode, obj.ErrorMsg)
	}
	code, message, ok := strings.Cut(e.Message, ": ")
	if ok && requestpolicy.ExplicitContentBlock(e.Status, code, message) {
		return true
	}
	return requestpolicy.ExplicitContentBlock(e.Status, "", e.Message)
}
