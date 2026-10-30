package workbuddy

// Wire contract adapted from workbuddy2api-panel internal/upstream/tasks.go
// (MIT; LICENSE.workbuddy). Only default personal CN tasks are supported here.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"codearts2api/internal/provider"
)

const GrowthTasksPath = "/v2/activity/growth/tasks"

func (c *Client) SupportsGrowthTasks(raw json.RawMessage) bool {
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return false
	}
	a, err := decode(normalized)
	return err == nil && a.Realm == "cn" && a.EnterpriseID == ""
}

func (c *Client) growthJSON(ctx context.Context, raw json.RawMessage, method, path string) (json.RawMessage, error) {
	return c.growthRequest(ctx, raw, method, path, nil, method == http.MethodPost)
}

func (c *Client) growthRequest(ctx context.Context, raw json.RawMessage, method, path string, payload []byte, web bool) (json.RawMessage, error) {
	if !c.SupportsGrowthTasks(raw) {
		return nil, fmt.Errorf("成长任务仅支持 WorkBuddy 国内个人账号")
	}
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	a, _ := decode(normalized)
	base := c.CNBase
	if web {
		base = c.CNWeb
	}
	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("WorkBuddy 成长任务请求无效")
	}
	c.headers(req, a)
	if web {
		req.Header.Set("Origin", "https://www.workbuddy.cn")
		req.Header.Set("Referer", "https://www.workbuddy.cn/profile/growth-center")
		req.Header.Set("X-Client-Platform", "web")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, provider.NewTransportError("WorkBuddy 成长任务", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return nil, fmt.Errorf("WorkBuddy 成长任务响应读取失败，结果未确认")
	}
	if err = upstreamError(resp.StatusCode, body); err != nil {
		return nil, err
	}
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &envelope) != nil || envelope.Code == nil || *envelope.Code != 0 {
		return nil, fmt.Errorf("WorkBuddy 成长任务响应缺少明确业务结果")
	}
	data := bytes.TrimSpace(envelope.Data)
	commandAck := !web && method == http.MethodPost && (path == GrowthAcceptPath || path == "/v2/report") && (len(data) == 0 || bytes.Equal(data, []byte("null")))
	if !bytes.HasPrefix(data, []byte("{")) && !commandAck {
		return nil, fmt.Errorf("WorkBuddy 成长任务响应结构无效")
	}
	return envelope.Data, nil
}

func (c *Client) ListGrowthTasks(ctx context.Context, raw json.RawMessage) ([]provider.GrowthTask, error) {
	data, err := c.growthJSON(ctx, raw, http.MethodGet, GrowthTasksPath)
	if err != nil {
		return nil, err
	}
	return parseGrowthTasks(data)
}

func parseGrowthTasks(data json.RawMessage) ([]provider.GrowthTask, error) {
	var envelope struct {
		Tasks json.RawMessage `json:"tasks"`
	}
	if json.Unmarshal(data, &envelope) != nil || !bytes.HasPrefix(bytes.TrimSpace(envelope.Tasks), []byte("[")) {
		return nil, fmt.Errorf("WorkBuddy 成长任务列表结构无效")
	}
	var items []struct {
		provider.GrowthTask
		RewardCredit int64           `json:"reward_credit"`
		RewardEnergy int64           `json:"reward_energy"`
		Progress     json.RawMessage `json:"progress"`
	}
	if json.Unmarshal(envelope.Tasks, &items) != nil || len(items) > 256 {
		return nil, fmt.Errorf("WorkBuddy 成长任务字段无效")
	}
	out := make([]provider.GrowthTask, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		t := item.GrowthTask
		if !provider.ValidGrowthTaskCode(t.Code) || seen[t.Code] {
			return nil, fmt.Errorf("WorkBuddy 成长任务标识无效或重复")
		}
		seen[t.Code] = true
		if len(item.Progress) > 0 && string(item.Progress) != "null" {
			var progress struct {
				Current *int64 `json:"current"`
				Target  *int64 `json:"target"`
			}
			if json.Unmarshal(item.Progress, &progress) != nil {
				return nil, fmt.Errorf("WorkBuddy 成长任务进度无效")
			}
			if progress.Current != nil {
				t.Current = *progress.Current
			}
			if progress.Target != nil {
				t.Target = *progress.Target
			}
		}
		t.Credit, t.Energy = item.RewardCredit, item.RewardEnergy
		if t.Current < 0 || t.Target < 0 || t.Credit < 0 || t.Energy < 0 {
			return nil, fmt.Errorf("WorkBuddy 成长任务数值无效")
		}
		if strings.TrimSpace(t.Title) == "" {
			t.Title = t.Code
		}
		t.Claimed = t.AcceptStatus == "claimed" || t.Status == "claimed"
		t.Claimable = !t.Locked && !t.Claimed && t.AcceptStatus != "not_accepted" && t.Target > 0 && t.Current >= t.Target
		annotateGrowthTask(&t)
		out = append(out, t)
	}
	return out, nil
}

func (c *Client) ClaimGrowthReward(ctx context.Context, raw json.RawMessage, code string) (provider.GrowthReward, error) {
	if !provider.ValidGrowthTaskCode(code) {
		return provider.GrowthReward{}, fmt.Errorf("成长任务标识无效")
	}
	data, err := c.growthJSON(ctx, raw, http.MethodPost, "/activity/growth/tasks/"+url.PathEscape(code)+"/claim")
	if err != nil {
		return provider.GrowthReward{}, err
	}
	var result struct {
		AlreadyClaimed *bool  `json:"already_claimed"`
		Credit         *int64 `json:"credit"`
		Energy         *int64 `json:"energy"`
	}
	if json.Unmarshal(data, &result) != nil || result.AlreadyClaimed == nil && result.Credit == nil && result.Energy == nil {
		return provider.GrowthReward{}, fmt.Errorf("WorkBuddy 成长奖励结果未确认，请刷新任务后核对")
	}
	reward := provider.GrowthReward{}
	if result.AlreadyClaimed != nil {
		reward.AlreadyClaimed = *result.AlreadyClaimed
	}
	if !reward.AlreadyClaimed {
		if result.Credit != nil {
			reward.Credit = *result.Credit
		}
		if result.Energy != nil {
			reward.Energy = *result.Energy
		}
	}
	if reward.Credit < 0 || reward.Energy < 0 {
		return provider.GrowthReward{}, fmt.Errorf("WorkBuddy 成长奖励数值无效")
	}
	return reward, nil
}

var _ provider.GrowthTasks = (*Client)(nil)
