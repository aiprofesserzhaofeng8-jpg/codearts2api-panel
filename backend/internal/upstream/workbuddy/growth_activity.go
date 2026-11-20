package workbuddy

// Activity wire format references workbuddy2api-panel (MIT; LICENSE.workbuddy).
// Only explicit growth actions use this maintenance protocol. No model request
// is made, and an acknowledged report never proves progress or a reward.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"codearts2api/internal/provider"
)

func (c *Client) growthMaintenanceHeaders(r *http.Request, a Credentials) {
	r.Header.Set("Authorization", "Bearer "+a.AccessToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Accept-Language", "zh-CN")
	r.Header.Set("X-CodeBuddy-Request", "1")
	r.Header.Set("User-Agent", "WorkBuddy/5.5.4")
	r.Header.Set("X-User-Id", a.UID)
	if a.Domain != "" {
		r.Header.Set("X-Domain", a.Domain)
	}
	if a.DeviceToken != "" {
		r.Header.Set("X-Device-Token", a.DeviceToken)
	}
}

func (c *Client) ReportGrowthActivity(ctx context.Context, raw json.RawMessage, task provider.GrowthTask) error {
	action, ok := c.GrowthAction(task.Code)
	if !ok || action.MaxReports == 0 || !c.SupportsGrowthTasks(raw) {
		return fmt.Errorf("该任务不支持活跃上报")
	}
	if task.Code != "first_buddy" && task.Code != "chat_5" {
		return c.growthDesktopActivity(ctx, raw, task)
	}
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	a, _ := decode(normalized)
	id, now := "wb2api-"+randomID(), time.Now().UnixMilli()
	event := map[string]any{
		"eventCode": "chat_request_send", "timestamp": now, "presentAt": now, "reportDelay": 0,
		"mode": "craft", "conversationId": id, "requestId": id, "parentConversationId": id, "rootRequestId": id,
		"requestModelId": "deepseek-v4-flash", "requestModelName": "DeepSeek V4 Flash", "inputLength": 12,
		"isPlan": false, "isAutoExecuteTerminal": false, "isAutoModify": false, "codebaseEnable": false,
		"maxToken": 0, "maxSteps": 0, "temperature": 0, "maxRetries": 0,
		"mentionContexts": []any{}, "knowledgeId": []any{}, "knowledgeName": []any{},
		"codebaseId": "", "mentionContextCount": 0, "command": "", "expertId": "", "recommendId": "",
		"skillId": "", "skillCount": 0, "totalCount": 0, "fileUri": "", "traceId": "",
		"agentName": "default", "agentType": "conversation", "userId": a.UID,
	}
	payload, _ := json.Marshal([]any{event})
	_, err = c.growthRequestAt(ctx, normalized, http.MethodPost, "/v2/report", payload, false, c.CNBilling)
	return err
}

var _ provider.GrowthActivityActions = (*Client)(nil)
