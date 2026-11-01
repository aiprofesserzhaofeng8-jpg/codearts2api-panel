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

// GrowthActions is separate from listing/claiming. Model tasks and activity
// tasks have independent limits; only model tasks use billable checkpoints.
type GrowthActions interface {
	AcceptGrowthTasks(context.Context, json.RawMessage, []string) error
	GrowthAction(string) (GrowthAction, bool)
	RunGrowthChat(context.Context, json.RawMessage, string, func([]byte) error) (bool, error)
}

type GrowthAction struct {
	Model string
	// NativeModel is a task-only upstream alias absent from the public catalog.
	NativeModel  bool
	MaxCalls     int64
	MaxReports   int64
	Prerequisite string
}

// GrowthActivityActions is optional. WorkBuddy growth activity uses a separate
// maintenance protocol, never the normal inference request path.
type GrowthActivityActions interface {
	ReportGrowthActivity(context.Context, json.RawMessage, GrowthTask) error
}

// GrowthChatSteps resolves task-specific resources before acquiring a model
// lease/checkpoint. Provider data never enters normal inference or persistence.
type GrowthChatSteps interface {
	PrepareGrowthChat(context.Context, json.RawMessage, GrowthTask) (GrowthChatStep, error)
	RunGrowthChatStep(context.Context, json.RawMessage, GrowthChatStep, func([]byte) error) (bool, error)
}

type GrowthChatStep struct {
	Task GrowthTask
	Data json.RawMessage
}

// GrowthBuddyOperations is optional and only used by explicit WorkBuddy task
// actions. Other providers, daily rewards and normal inference do not use it.
type GrowthBuddyOperations interface {
	GrowthBuddyStatus(context.Context, json.RawMessage) (GrowthBuddyStatus, error)
	AgreeGrowthBuddy(context.Context, json.RawMessage) error
	AdoptGrowthBuddy(context.Context, json.RawMessage) error
}

type GrowthBuddyStatus struct {
	Owned  bool `json:"owned"`
	Agreed bool `json:"agreed"`
}

type GrowthTask struct {
	Code              string `json:"task_code"`
	Title             string `json:"title"`
	Description       string `json:"description,omitempty"`
	TaskDesc          string `json:"task_desc,omitempty"`
	JumpURL           string `json:"jump_url,omitempty"`
	Credit            int64  `json:"credit"`
	Energy            int64  `json:"energy"`
	RewardBuddy       bool   `json:"reward_buddy,omitempty"`
	Current           int64  `json:"current"`
	Target            int64  `json:"target"`
	AcceptStatus      string `json:"accept_status,omitempty"`
	AcceptNotRequired bool   `json:"accept_not_required,omitempty"`
	Status            string `json:"status,omitempty"`
	Locked            bool   `json:"locked"`
	Claimed           bool   `json:"claimed"`
	Claimable         bool   `json:"claimable"`
	AutoSupported     bool   `json:"auto_supported,omitempty"`
	ActivityOnly      bool   `json:"activity_only,omitempty"`
	ActionDescription string `json:"action_description,omitempty"`
	ManualInstruction string `json:"manual_instruction,omitempty"`
	ExecutionPending  bool   `json:"execution_pending,omitempty"`
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
