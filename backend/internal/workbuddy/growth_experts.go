package workbuddy

// Resource resolution and event composition for billable growth tasks. The
// server owns leases, durable guards and accounting; a step makes one chat.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"codearts2api/internal/provider"
)

func growthExpertID(link string) string {
	if len(link) > 4096 {
		return ""
	}
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	if u.Scheme == "https" && u.Host == "www.workbuddy.cn" {
		u, err = url.Parse(u.Query().Get("deeplink"))
		if err != nil {
			return ""
		}
	}
	if u.Scheme != "workbuddy" || (u.Host != "expert" && u.Host != "experts") {
		return ""
	}
	id := u.Query().Get("expertId")
	if !strings.HasPrefix(id, "ex_") || !provider.ValidGrowthTaskCode(id) {
		return ""
	}
	return id
}
func (c *Client) growthExperts(ctx context.Context, raw json.RawMessage, kind, keyword string) ([]growthMarketExpert, error) {
	body, _ := json.Marshal(map[string]any{"page": 1, "page_size": 20, "sort_by": "reco_rank", "sort_order": "desc", "expert_type": kind, "keyword": keyword})
	data, err := c.growthRequestWithHeaders(ctx, raw, http.MethodPost, "/portal/operation-platform/market/expert/list", body, false, c.CNBase, c.growthDesktopHeaders)
	if err != nil {
		return nil, fmt.Errorf("专家资源查询失败：%w", err)
	}
	var result struct {
		Experts []growthMarketExpert `json:"experts"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Experts) > 100 {
		return nil, fmt.Errorf("专家资源列表格式无效")
	}
	seen := map[string]bool{}
	out := make([]growthMarketExpert, 0, len(result.Experts))
	for _, e := range result.Experts {
		if !strings.HasPrefix(e.ExpertID, "ex_") || !provider.ValidGrowthTaskCode(e.ExpertID) || seen[e.ExpertID] || e.ExpertType != kind {
			continue
		}
		seen[e.ExpertID] = true
		out = append(out, e)
	}
	return out, nil
}
func (c *Client) PrepareGrowthChat(ctx context.Context, raw json.RawMessage, task provider.GrowthTask) (provider.GrowthChatStep, error) {
	step := provider.GrowthChatStep{Task: task}
	if task.Code == "Model_chat_GLM5.2" {
		return step, nil
	}
	spec, ok := growthSpec(task.Code)
	if !ok || spec.action.MaxCalls == 0 || !c.SupportsGrowthTasks(raw) {
		return step, fmt.Errorf("该任务不支持模型执行")
	}
	if task.Code == "skill_1" {
		return step, nil
	}
	kind := spec.expertType
	if kind == "" {
		kind = "agent"
	}
	experts, err := c.growthExperts(ctx, raw, kind, spec.expertName)
	if err != nil {
		return step, err
	}
	var selected *growthMarketExpert
	if spec.expertName != "" {
		id := growthExpertID(task.JumpURL)
		if strings.HasPrefix(task.Code, "expert_use_") && id == "" {
			return step, fmt.Errorf("指定专家任务缺少有效官方跳转标识，未开始对话")
		}
		for i := range experts {
			if id != "" && experts[i].ExpertID == id || id == "" && experts[i].DisplayNameZH == spec.expertName {
				selected = &experts[i]
				break
			}
		}
	} else if task.Current >= 0 && task.Current < int64(len(experts)) {
		selected = &experts[task.Current]
	}
	if selected == nil {
		return step, fmt.Errorf("未找到任务要求的真实专家，未开始对话；请刷新或到官方专家页核对")
	}
	if err = c.growthDesktopReport(ctx, raw, growthDesktopExpertSummonSequence(*selected)); err != nil {
		return step, fmt.Errorf("专家召唤上报未确认：%w", err)
	}
	step.Data, _ = json.Marshal(selected)
	return step, nil
}
func (c *Client) RunGrowthChatStep(ctx context.Context, raw json.RawMessage, step provider.GrowthChatStep, onChunk func([]byte) error) (bool, error) {
	if step.Task.Code == "Model_chat_GLM5.2" {
		return c.RunGrowthChat(ctx, raw, step.Task.Code, onChunk)
	}
	spec, ok := growthSpec(step.Task.Code)
	if !ok || spec.action.MaxCalls == 0 {
		return false, fmt.Errorf("该任务不支持模型执行")
	}
	var expert growthMarketExpert
	if step.Task.Code != "skill_1" && (json.Unmarshal(step.Data, &expert) != nil || expert.ExpertID == "") {
		return false, fmt.Errorf("专家执行资源缺失")
	}
	conv, req, completed, err := c.growthDesktopChat(ctx, raw, expert.ExpertID, onChunk)
	if err != nil {
		return completed, err
	}
	events := growthDesktopChatSequence(conv, req, "msg-"+req, "fast-model", "fast-model")
	if step.Task.Code == "skill_1" {
		for _, e := range events {
			if e["eventCode"] == "chat_message_response" {
				e["finishReason"] = "tool_calls"
			}
		}
		events = append(events, growthDesktopEvent{"eventCode": "skill_info", "id": "润泽小馆·日报撰写", "skillId": "skill_2097350077599879168", "skillVersion": "1.0.0", "toolStatus": "success", "fileCount": 56, "source": "workbuddy-desktop", "conversationId": conv, "requestId": req, "messageId": "msg-" + req, "requestModelId": "fast-model", "requestModelName": "fast-model", "traceId": req})
	} else {
		for _, e := range events {
			if e["eventCode"] == "agent_task_created" {
				e["has_expert"] = true
				e["expert_id"] = expert.ExpertID
				e["expert_name"] = expert.DisplayNameZH
			}
		}
		use := growthDesktopExpertActualUse(expert, conv, req)
		use["mode"] = "craft"
		if step.Task.Code == "Expert_lighthouse" {
			use["mode"], use["type"], use["cost"] = "LOCAL", "", 0
		}
		events = append(events, use)
	}
	if err = c.growthDesktopReport(ctx, raw, events); err != nil {
		return true, fmt.Errorf("真实对话已完成，使用事件尚未确认：%w", err)
	}
	return true, nil
}

var _ provider.GrowthChatSteps = (*Client)(nil)
