package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SummaryFailure is request-scoped: an incomplete summary must not multiply
// attempts through account failover or poison an otherwise usable account.
type SummaryFailure struct{ Message string }

func (e *SummaryFailure) Error() string             { return e.Message }
func (e *SummaryFailure) NonRetryableRequest() bool { return true }

// SummaryAttempt contains an already-authorized physical model and prepared
// body. Prepare must retain the bound account and its existing request budget.
type SummaryAttempt struct {
	Model string
	Body  []byte
}

// SummaryHooks retain the caller's policy, accounting and health boundaries.
// Start runs only after Prepare succeeds. Usage receives each valid JSON frame
// before the size check; Result runs on transport failure or complete success,
// not on a truncated, empty or filtered response.
type SummaryHooks struct {
	Prepare func(retry bool) (SummaryAttempt, error)
	Start   func(number int, model string)
	Usage   func(context.Context, []byte)
	Result  func(model string, err error)
}

// RunSummary buffers privately until a complete textual summary exists. Only
// an output-limit finish can trigger one retry, on the same bound transport.
// No chunks reach the caller when either attempt fails validation.
func RunSummary(ctx context.Context, session StreamTransport, retry bool, hooks SummaryHooks) ([][]byte, error) {
	attempts := 1
	if retry {
		attempts = 2
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prepared, err := hooks.Prepare(attempt > 0)
		if err != nil {
			return nil, err
		}
		hooks.Start(attempt+1, prepared.Model)
		var chunks [][]byte
		finish, textChars, toolCall, size := "", 0, false, 0
		err = session.Stream(ctx, prepared.Model, prepared.Body, func(raw []byte) error {
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
			hooks.Usage(ctx, raw)
			for _, c := range chunk.Choices {
				if c.Finish != "" {
					finish = c.Finish
				}
				textChars += len(strings.TrimSpace(c.Delta.Content))
				toolCall = toolCall || len(c.Delta.Tools) > 0
			}
			size += len(raw)
			if size > 8<<20 {
				return &SummaryFailure{"摘要响应超过 8 MiB，已终止；请缩小待压缩历史"}
			}
			chunks = append(chunks, append([]byte(nil), raw...))
			return nil
		})
		if err != nil {
			hooks.Result(prepared.Model, err)
			return nil, &SummaryFailure{"摘要请求失败，已停止自动换号重试：" + err.Error()}
		}
		if finish == "stop" && textChars > 0 && !toolCall {
			hooks.Result(prepared.Model, nil)
			return chunks, nil
		}
		// Preserve the existing finish-reason gate, including unexpected tool
		// output with a length finish. Tool output is never released as a summary.
		if finish != "length" {
			return nil, &SummaryFailure{"摘要未完整结束或没有有效正文（结束原因：" + finish + "），未返回不完整摘要"}
		}
		if attempt+1 == attempts {
			return nil, &SummaryFailure{fmt.Sprintf("摘要连续 %d 次达到输出上限，已停止重试且未返回不完整摘要；请减少待压缩历史或调整摘要预算", attempts)}
		}
	}
	return nil, &SummaryFailure{"摘要未生成"}
}
