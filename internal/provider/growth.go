package provider

import (
	"context"
	"encoding/json"
)

// GrowthTasks is optional and independent from daily check-in and inference.
// Lists are read-only; claiming must never perform actions to earn progress.
type GrowthTasks interface {
	SupportsGrowthTasks(json.RawMessage) bool
	ListGrowthTasks(context.Context, json.RawMessage) ([]GrowthTask, error)
	ClaimGrowthReward(context.Context, json.RawMessage, string) (GrowthReward, error)
}

type GrowthTask struct {
	Code         string `json:"task_code"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	TaskDesc     string `json:"task_desc,omitempty"`
	Credit       int64  `json:"credit"`
	Energy       int64  `json:"energy"`
	RewardBuddy  bool   `json:"reward_buddy,omitempty"`
	Current      int64  `json:"current"`
	Target       int64  `json:"target"`
	AcceptStatus string `json:"accept_status,omitempty"`
	Status       string `json:"status,omitempty"`
	Locked       bool   `json:"locked"`
	Claimed      bool   `json:"claimed"`
	Claimable    bool   `json:"claimable"`
}

type GrowthReward struct {
	AlreadyClaimed bool  `json:"already_claimed"`
	Credit         int64 `json:"credit"`
	Energy         int64 `json:"energy"`
}

func ValidGrowthTaskCode(code string) bool {
	if len(code) == 0 || len(code) > 128 {
		return false
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return code != "." && code != ".."
}
