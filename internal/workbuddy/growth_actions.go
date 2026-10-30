package workbuddy

// Enrollment/report wire shapes reference workbuddy2api-panel (MIT;
// LICENSE.workbuddy). Completion reports here require a successful real chat.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"codearts2api/internal/provider"
)

const GrowthAcceptPath = GrowthTasksPath + "/accept"
const growthPrompt = "请用一句话介绍你自己。"

func (c *Client) GrowthAction(code string) (provider.GrowthAction, bool) {
	switch code {
	case "Model_chat_GLM5.2":
		return provider.GrowthAction{Model: "glm-5.2", MaxCalls: 1}, true
	case "chat_5":
		return provider.GrowthAction{Model: "glm-5.2", MaxCalls: 5}, true
	default:
		return provider.GrowthAction{}, false
	}
}

func annotateGrowthTask(t *provider.GrowthTask) {
	// Never trust upstream fields that advertise local execution capabilities.
	t.AutoSupported, t.ActionDescription, t.ManualInstruction, t.ExecutionPending = false, "", "", false
	switch t.Code {
	case "Model_chat_GLM5.2":
		t.AutoSupported = true
		t.ActionDescription = "进行一次 GLM-5.2 真实对话，最多输出 32 Token，会消耗积分"
	case "chat_5":
		t.AutoSupported = true
		t.ActionDescription = "使用 GLM-5.2 按差额进行真实对话，最多 5 次，每次最多输出 32 Token，会消耗积分"
	case "first_buddy":
		t.ManualInstruction = "请在官方应用阅读并同意领养协议，再领养 Buddy"
	case "Expert_Philanthropy":
		t.ManualInstruction = "涉及捐款，请在官方应用自行决定是否参与"
	default:
		t.ManualInstruction = "请按任务说明在官方应用完成，随后刷新进度并领取达标奖励"
	}
}

func (c *Client) AcceptGrowthTasks(ctx context.Context, raw json.RawMessage, codes []string) error {
	if len(codes) == 0 || len(codes) > 20 {
		return fmt.Errorf("每次接取 1 至 20 项成长任务")
	}
	seen := map[string]bool{}
	for _, code := range codes {
		if !provider.ValidGrowthTaskCode(code) || seen[code] {
			return fmt.Errorf("成长任务标识无效或重复")
		}
		seen[code] = true
	}
	body, _ := json.Marshal(map[string]any{"task_codes": codes})
	_, err := c.growthRequest(ctx, raw, http.MethodPost, GrowthAcceptPath, body, false)
	return err
}

// RunGrowthChat makes exactly one real model call. The caller owns account
// leases, durable execution guards, accounting and progress/claim confirmation.
// The bool reports whether the chat completed even if reporting subsequently fails.
func (c *Client) RunGrowthChat(ctx context.Context, raw json.RawMessage, code string, onChunk func([]byte) error) (bool, error) {
	action, ok := c.GrowthAction(code)
	if !ok || !c.SupportsGrowthTasks(raw) {
		return false, fmt.Errorf("该任务需要在官方应用完成")
	}
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return false, err
	}
	a, _ := decode(normalized)
	session, request := randomID(), randomID()
	body, _ := json.Marshal(map[string]any{"model": action.Model, "messages": []map[string]string{{"role": "user", "content": growthPrompt}}, "max_tokens": 32, "stream": true, "thinking": map[string]string{"type": "disabled"}})
	var responseID string
	var size int
	var content bool
	err = c.Stream(ctx, normalized, provider.Request{Model: action.Model, Body: body, SessionID: session, RequestID: request}, func(b []byte) error {
		if err := onChunk(b); err != nil {
			return err
		}
		size += len(b)
		if size > 256<<10 {
			return fmt.Errorf("成长任务对话响应超过限制")
		}
		var frame struct {
			ID      string `json:"id"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(b, &frame) != nil {
			return fmt.Errorf("成长任务对话响应无效")
		}
		if frame.ID != "" {
			if len(frame.ID) > 256 || responseID != "" && responseID != frame.ID {
				return fmt.Errorf("成长任务对话标识无效")
			}
			responseID = frame.ID
		}
		for _, choice := range frame.Choices {
			content = content || strings.TrimSpace(choice.Delta.Content) != ""
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if responseID == "" || !content {
		return true, fmt.Errorf("真实对话缺少有效内容或上游标识，未上报任务进度")
	}
	// Match the conversation ID actually sent by conversationHeaders. requestId
	// comes from the upstream response, never a fabricated completion identifier.
	sum := sha256.Sum256([]byte(session))
	conversationID := hex.EncodeToString(sum[:16])
	now := time.Now().UnixMilli()
	event := map[string]any{"eventCode": "chat_request_send", "timestamp": now, "presentAt": now, "reportDelay": 0, "mode": "craft", "conversationId": conversationID, "parentConversationId": conversationID, "requestId": responseID, "rootRequestId": responseID, "inputLength": utf8.RuneCountInString(growthPrompt), "requestModelId": action.Model, "requestModelName": "GLM-5.2", "maxToken": 32, "maxRetries": 0, "userId": a.UID, "agentName": "default", "agentType": "conversation"}
	payload, _ := json.Marshal([]any{event})
	_, err = c.growthRequest(ctx, normalized, http.MethodPost, "/v2/report", payload, false)
	if err != nil {
		return true, fmt.Errorf("真实对话已完成，进度上报未确认：%w", err)
	}
	return true, nil
}

var _ provider.GrowthActions = (*Client)(nil)
