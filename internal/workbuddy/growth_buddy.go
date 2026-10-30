package workbuddy

// Buddy endpoints reference workbuddy2api-panel (MIT; LICENSE.workbuddy).
// Status is authoritative; a successful write alone never proves adoption.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"codearts2api/internal/provider"
)

const (
	GrowthBuddyInfoPath      = "/activity/growth/buddy/info"
	GrowthBuddyAgreementPath = "/activity/growth/buddy/agreement"
	GrowthBuddyFirstPath     = "/activity/growth/buddy/first"
)

func (c *Client) GrowthBuddyStatus(ctx context.Context, raw json.RawMessage) (provider.GrowthBuddyStatus, error) {
	var status provider.GrowthBuddyStatus
	data, err := c.growthRequest(ctx, raw, http.MethodGet, GrowthBuddyInfoPath, nil, false)
	if err != nil {
		return status, err
	}
	var info struct {
		Buddy json.RawMessage `json:"buddy"`
	}
	if json.Unmarshal(data, &info) != nil || len(info.Buddy) == 0 {
		return status, fmt.Errorf("Buddy 领养状态未确认，请刷新核对")
	}
	if !bytes.Equal(bytes.TrimSpace(info.Buddy), []byte("null")) {
		var buddy struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(info.Buddy, &buddy) != nil {
			return status, fmt.Errorf("Buddy 档案格式无效")
		}
		id, err := strconv.ParseInt(string(bytes.Trim(buddy.ID, `"`)), 10, 64)
		if err != nil || id <= 0 {
			return status, fmt.Errorf("Buddy 档案标识无效，领养尚未确认")
		}
		status.Owned = true
		return status, nil
	}
	data, err = c.growthRequest(ctx, raw, http.MethodGet, GrowthBuddyAgreementPath, nil, false)
	if err != nil {
		return status, err
	}
	var agreement struct {
		Agreed *bool `json:"agreed"`
	}
	if json.Unmarshal(data, &agreement) != nil || agreement.Agreed == nil {
		return status, fmt.Errorf("Buddy 协议状态未确认，请刷新核对")
	}
	status.Agreed = *agreement.Agreed
	return status, nil
}

func (c *Client) AgreeGrowthBuddy(ctx context.Context, raw json.RawMessage) error {
	_, err := c.growthRequest(ctx, raw, http.MethodPost, GrowthBuddyAgreementPath, []byte(`{"agree":true}`), false)
	return err
}

func (c *Client) AdoptGrowthBuddy(ctx context.Context, raw json.RawMessage) error {
	_, err := c.growthRequest(ctx, raw, http.MethodPost, GrowthBuddyFirstPath, []byte(`{}`), false)
	return err
}

var _ provider.GrowthBuddyOperations = (*Client)(nil)

func buddyTaskIncomplete(body []byte) bool {
	var envelope struct {
		Message string `json:"message"`
		Msg     string `json:"msg"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	return strings.Contains(strings.ToLower(envelope.Message+" "+envelope.Msg+" "+envelope.Error.Message), "first_buddy task not completed yet")
}
