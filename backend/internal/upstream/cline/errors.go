// SPDX-License-Identifier: LGPL-3.0-only
// Structured error classes adapted from Sub2API ratelimit_cline.go.
package cline

import (
	"codearts2api/internal/provider"
	"encoding/json"
	"strings"
)

// Use existing model-level cooldowns; never infer an account-wide ban from a
// wallet error or invent a reset time. Do not expose upstream request echoes.
func classifyFailure(body []byte, fallback error) error {
	e, ok := fallback.(*provider.Error)
	if !ok || e.Status == 401 {
		return fallback
	}
	var obj struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &obj) != nil {
		return fallback
	}
	code := strings.ToLower(obj.Error.Code)
	if strings.EqualFold(obj.Error.Details.Code, "SPEND_LIMIT_EXCEEDED") {
		code = "spend_limit_exceeded"
	}
	msg := strings.ToLower(obj.Error.Message + " " + obj.Error.Details.Message)
	switch {
	case code == "insufficient_credits":
		e.Code = "INSUFFICIENT_CREDITS"
		e.Message = "Cline 按量积分不足"
		e.Status = 429
	case code == "spend_limit_exceeded":
		e.Code = "SPEND_LIMIT_EXCEEDED"
		e.Message = "Cline 花费上限已达到"
		e.Status = 429
	case code == "inference_cap_error":
		e.Code = "INFERENCE_CAP_ERROR"
		e.Message = "Cline 当前模型额度已达到上限"
		e.Status = 429
	case strings.Contains(msg, "clinepass limit") && strings.Contains(msg, "you have reached your"):
		e.Code = "CLINE_PASS_LIMIT"
		e.Message = "ClinePass 窗口额度已达到上限"
		e.Status = 429
	case strings.Contains(msg, "free limit reached on model"):
		e.Code = "CLINE_FREE_LIMIT"
		e.Message = "Cline 当前免费模型额度已达到上限"
		e.Status = 429
	case strings.Contains(msg, "the user is not subscribed to required model plan"), strings.Contains(msg, "organization accounts cannot use individual model inference subscriptions"):
		e.Code = "CLINE_PASS_UNAVAILABLE"
		e.Message = "ClinePass 订阅不可用于当前模型"
		e.Status = 403
	default:
		return fallback
	}
	e.AccountFault = false
	return e
}
