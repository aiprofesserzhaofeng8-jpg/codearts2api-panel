package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// Request-level failure: never poison an account or multiply summary attempts
// through the outer account failover loop.
type summaryFailure struct{ message string }

func (e *summaryFailure) Error() string             { return e.message }
func (e *summaryFailure) NonRetryableRequest() bool { return true }

func (s *Server) summaryPolicy(ctx context.Context, body []byte, model string) (store.ModelPolicy, bool) {
	explicit, _ := ctx.Value(summaryIntentKey{}).(bool)
	p, ok := s.Store.ModelPolicy(model)
	return p, ok && p.SummaryEnabled && summaryRequest(body, explicit)
}

// Retain private chunks until a complete textual summary exists. This makes a
// single same-account retry safe even when the caller requested a stream.
func (s *Server) summaryChunks(ctx context.Context, sess *codearts.Session, model registry.Model, body []byte, p store.ModelPolicy) ([][]byte, error) {
	attempts := 1
	if p.SummaryRetry {
		attempts = 2
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target, policy := model, p
		if attempt > 0 && p.SummaryFallbackModel != "" {
			if forced := s.Store.Settings().ForceModel; forced != "" && forced != p.SummaryFallbackModel {
				return nil, &summaryFailure{"强制模型设置阻止摘要备用模型调用"}
			}
			var exists bool
			target, exists = s.Reg.Resolve(p.SummaryFallbackModel, nil)
			if !exists || modelProvider(target) != modelProvider(model) || target.Scope != model.Scope {
				return nil, &summaryFailure{"摘要备用模型已不在模型目录中"}
			}
			if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
				if s.Store.ModelHealthSnapshot()[c.accountID][target.ID].CooldownUntil > time.Now().Unix() {
					return nil, &summaryFailure{"摘要备用模型正在冷却，请调整策略或稍后再试"}
				}
			}
			if configured, exists := s.Store.ModelPolicy(target.ID); exists {
				policy = configured
			} else {
				policy.DisableSummaryThinking = false
			}
		}
		if attempt > 0 {
			if err := s.checkRequestBudget(ctx, target.ID); err != nil {
				return nil, err
			}
		}
		requestBody, err := rewriteModel(body, target.ID, true)
		if err != nil {
			return nil, err
		}
		if target.ID != model.ID {
			// Reasoning options for the original model are not a capability claim
			// about the explicitly selected fallback.
			var request map[string]json.RawMessage
			if err := json.Unmarshal(requestBody, &request); err != nil {
				return nil, err
			}
			delete(request, "thinking")
			delete(request, "reasoning_effort")
			requestBody = mustJSON(request)
		}
		request, err := adaptSummary(requestBody, policy, attempt > 0)
		if err != nil {
			return nil, err
		}
		if attempt > 0 {
			resetUsage(ctx)
			if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
				c.model = target.ID
			}
		}
		markSummary(ctx, attempt+1)
		var chunks [][]byte
		finish := ""
		textChars := 0
		toolCall := false
		size := 0
		err = s.Up.ChatStream(ctx, sess, target.ID, request, func(raw []byte) error {
			if msg, status, bad := codearts.IsUpstreamBusinessError(raw); bad {
				return codearts.UpstreamBusinessError(status, msg)
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string            `json:"content"`
						Tools   []json.RawMessage `json:"tool_calls"`
					} `json:"delta"`
					Finish string `json:"finish_reason"`
				} `json:"choices"`
			}
			if json.Unmarshal(raw, &chunk) != nil {
				return fmt.Errorf("摘要上游返回了无效 JSON")
			}
			captureUsage(ctx, raw)
			for _, c := range chunk.Choices {
				if c.Finish != "" {
					finish = c.Finish
				}
				textChars += len(strings.TrimSpace(c.Delta.Content))
				toolCall = toolCall || len(c.Delta.Tools) > 0
			}
			size += len(raw)
			if size > 8<<20 {
				return &summaryFailure{"摘要响应超过 8 MiB，已终止；请缩小待压缩历史"}
			}
			chunks = append(chunks, append([]byte(nil), raw...))
			return nil
		})
		if err != nil {
			if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok && (codearts.IsTransientUpstream(err) || modelUnavailable(err)) {
				s.Store.MarkModelResult(c.accountID, target.ID, false, "摘要上游暂不可用", s.Store.Settings().Pool.RateCooldownSec)
			}
			return nil, &summaryFailure{"摘要请求失败，已停止自动换号重试：" + err.Error()}
		}
		if finish == "stop" && textChars > 0 && !toolCall {
			if c, ok := ctx.Value(usageContextKey{}).(*usageCapture); ok {
				s.Store.MarkModelResult(c.accountID, target.ID, true, "", 0)
			}
			return chunks, nil
		}
		// No retries for malformed, filtered, empty or tool-executing summaries.
		if finish != "length" {
			return nil, &summaryFailure{"摘要未完整结束或没有有效正文（结束原因：" + finish + "），未返回不完整摘要"}
		}
		if attempt+1 == attempts {
			return nil, &summaryFailure{fmt.Sprintf("摘要连续 %d 次达到输出上限，已停止重试且未返回不完整摘要；请减少待压缩历史或调整摘要预算", attempts)}
		}
	}
	return nil, &summaryFailure{"摘要未生成"}
}
