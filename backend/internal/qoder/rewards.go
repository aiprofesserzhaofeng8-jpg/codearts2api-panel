package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"codearts2api/internal/provider"
)

const CampaignsPath = "/sash/api/v1/me/campaigns"

var campaignID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

type rewardCampaign struct {
	ID        string `json:"campaignId"`
	Action    string `json:"actionType"`
	Status    string `json:"claimStatus"`
	Start     int64  `json:"startAt"`
	End       int64  `json:"endAt"`
	Required  string `json:"requiredAchievementKey"`
	Completed bool   `json:"achievementCompleted"`
	Benefit   *struct {
		Kind   string  `json:"kind"`
		Amount float64 `json:"amount"`
	} `json:"benefit"`
}

// Reward traffic uses the billing bearer protocol, never the COSY chat signature.
// No machine identities, campaign IDs or achievement events are synthesized.
func (c *Client) rewardRequest(ctx context.Context, a Credentials, path, method string) ([]byte, error) {
	region, ok := c.Regions[a.Scope]
	if !ok {
		return nil, fmt.Errorf("Qoder 地区无效")
	}
	var body []byte
	if method == http.MethodPost {
		body = []byte(`{}`)
	}
	req, err := http.NewRequestWithContext(ctx, method, region.OpenAPI+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Qoder 活动地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Qoder")
	req.Header.Set("Cosy-ClientType", "10")
	req.Header.Set("Cosy-Version", Version)
	req.Header.Set("Cache-Control", "no-store")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	b, err := readJSON(resp)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil || obj == nil {
		return nil, failure(502)
	}
	if string(obj["success"]) == "false" {
		return nil, failure(502)
	}
	if code := string(obj["code"]); code != "" && code != "null" && code != "0" && code != `"0"` && code != "200" && code != `"200"` {
		return nil, failure(502)
	}
	if data := obj["data"]; len(data) > 0 && string(data) != "null" {
		return data, nil
	}
	return b, nil
}

func (c *Client) campaigns(ctx context.Context, a Credentials) ([]rewardCampaign, error) {
	b, err := c.rewardRequest(ctx, a, CampaignsPath+"?forceRefresh=true", http.MethodGet)
	if err != nil {
		return nil, err
	}
	var data struct {
		Campaigns *[]rewardCampaign `json:"campaigns"`
	}
	if json.Unmarshal(b, &data) != nil || data.Campaigns == nil {
		return nil, failure(502)
	}
	if len(*data.Campaigns) > 128 {
		return nil, failure(502)
	}
	seen := make(map[string]bool)
	rows := make([]rewardCampaign, 0, len(*data.Campaigns))
	for _, row := range *data.Campaigns {
		if seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		rows = append(rows, row)
	}
	return rows, nil
}

func (r rewardCampaign) eligible(now int64) bool {
	start, end := r.Start, r.End
	if start > 1e12 {
		start /= 1000
	}
	if end > 1e12 {
		end /= 1000
	}
	return campaignID.MatchString(r.ID) && (r.Action == "" || strings.EqualFold(r.Action, "CLAIM_BENEFIT")) &&
		r.Benefit != nil && strings.EqualFold(r.Benefit.Kind, "CREDITS") && r.Benefit.Amount > 0 &&
		(r.Required == "" || r.Completed) && (start == 0 || now >= start) && (end == 0 || now < end)
}

func campaignResult(rows []rewardCampaign) provider.RewardResult {
	result := provider.RewardResult{State: "unavailable", Message: "Qoder 当前暂无可领取积分活动；可在官方客户端核对活动资格"}
	amount := 0.0
	for _, r := range rows {
		if !r.eligible(time.Now().Unix()) {
			continue
		}
		switch strings.ToUpper(r.Status) {
		case "CLAIMABLE":
			amount += r.Benefit.Amount
		case "CLAIMED":
			result.State = "already_claimed"
			result.Message = "Qoder 上游确认当前活动积分已领取"
		}
	}
	if amount > 0 {
		result.State = "claimable"
		result.Message = fmt.Sprintf("Qoder 当前可领取 %g Credits", amount)
		result.Credits = &amount
	}
	return result
}

func (c *Client) DailyStatus(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	a, err := decode(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	rows, err := c.campaigns(ctx, a)
	if err != nil {
		return provider.RewardResult{}, err
	}
	return campaignResult(rows), nil
}

func (c *Client) ClaimDaily(ctx context.Context, raw json.RawMessage) (provider.RewardResult, error) {
	a, err := decode(raw)
	if err != nil {
		return provider.RewardResult{}, err
	}
	rows, err := c.campaigns(ctx, a)
	if err != nil {
		return provider.RewardResult{}, err
	}
	before := campaignResult(rows)
	if before.State != "claimable" {
		return before, nil
	}
	// Each advertised eligible campaign is submitted at most once. Always requery
	// the same ID, including on transport errors; a different claimed row is not proof.
	total := 0.0
	for _, r := range rows {
		if !r.eligible(time.Now().Unix()) || !strings.EqualFold(r.Status, "CLAIMABLE") {
			continue
		}
		_, claimErr := c.rewardRequest(ctx, a, CampaignsPath+"/"+r.ID+"/claim", http.MethodPost)
		if ctx.Err() != nil {
			return provider.RewardResult{}, ctx.Err()
		}
		after, checkErr := c.campaigns(ctx, a)
		confirmed := false
		for _, v := range after {
			if v.ID == r.ID && strings.EqualFold(v.Status, "CLAIMED") {
				confirmed = true
				break
			}
		}
		if !confirmed {
			if claimErr != nil {
				return provider.RewardResult{}, claimErr
			}
			if checkErr != nil {
				return provider.RewardResult{State: "unknown", Message: "Qoder 已提交领取但复查失败，请稍后刷新状态"}, nil
			}
			return provider.RewardResult{State: "unknown", Message: "Qoder 已提交领取，上游尚未确认到账，请稍后刷新状态"}, nil
		}
		total += r.Benefit.Amount
	}
	return provider.RewardResult{State: "claimed", Message: fmt.Sprintf("Qoder 活动领取已复查确认（活动标示 %g Credits），最新余额将自动刷新", total), Credits: &total}, nil
}

var _ provider.DailyRewards = (*Client)(nil)
