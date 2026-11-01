package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

// Growth coordinates explicit management actions. It never owns credentials,
// model admission or usage accounting; PaidChat calls the existing charged path.
type Growth struct {
	path       string
	locks      accounts.OperationLocks
	execution  growthExecutionState
	invalidate func(string)
}

type GrowthOperations struct {
	Credit            func(context.Context, *accounts.Account, bool) (*accounts.CreditInfo, error)
	PaidChat          func(context.Context, *accounts.Account, provider.GrowthActions, json.RawMessage, provider.GrowthTask, string) (bool, error)
	AllowCreditModels func() bool
	ResolveModel      func(string) bool
}

// GrowthRejection describes an unchanged management precondition failure.
// Upstream/uncertain write failures are returned separately and never retried.
type GrowthRejection struct {
	Status  int
	Message string
}

func (e *GrowthRejection) Error() string { return e.Message }
func NewGrowth(path string, invalidate func(string)) *Growth {
	return &Growth{path: path, invalidate: invalidate}
}
func (g *Growth) Acquire(ctx context.Context, id string) (func(), error) {
	return g.locks.Acquire(ctx, id)
}
func (g *Growth) Checkpoint(id string, task provider.GrowthTask, remove bool) error {
	return g.execution.checkpoint(g.path, id, task, remove)
}
func (g *Growth) Pending(id string, task provider.GrowthTask) (bool, error) {
	return g.execution.pending(g.path, id, task)
}
func (g *Growth) Decorate(id string, tasks []provider.GrowthTask) {
	for i := range tasks {
		if tasks[i].AutoSupported && !tasks[i].ActivityOnly {
			pending, err := g.Pending(id, tasks[i])
			tasks[i].ExecutionPending = pending
			if err != nil {
				tasks[i].ActionDescription = err.Error()
			}
		}
	}
}

type GrowthResult struct {
	OK               bool                        `json:"ok"`
	ID               string                      `json:"id"`
	Tasks            []provider.GrowthTask       `json:"tasks"`
	State            string                      `json:"state,omitempty"`
	Message          string                      `json:"message,omitempty"`
	Reward           *provider.GrowthReward      `json:"reward,omitempty"`
	Balance          *accounts.CreditInfo        `json:"balance,omitempty"`
	BalanceError     string                      `json:"balance_error,omitempty"`
	CompletedCalls   int                         `json:"completed_calls,omitempty"`
	CompletedReports int                         `json:"completed_reports,omitempty"`
	Buddy            *provider.GrowthBuddyStatus `json:"buddy,omitempty"`
	BuddyError       string                      `json:"buddy_error,omitempty"`
}

func GrowthTask(tasks []provider.GrowthTask, code string) *provider.GrowthTask {
	for i := range tasks {
		if tasks[i].Code == code {
			return &tasks[i]
		}
	}
	return nil
}

func (g *Growth) List(ctx context.Context, id string, adapter provider.Adapter, raw json.RawMessage) (GrowthResult, error) {
	tasks, err := adapter.(provider.GrowthTasks).ListGrowthTasks(ctx, raw)
	if err != nil {
		return GrowthResult{}, err
	}
	g.Decorate(id, tasks)
	reply := GrowthResult{OK: true, ID: id, Tasks: tasks}
	if first := GrowthTask(tasks, "first_buddy"); first != nil && !first.Claimed {
		if buddy, ok := adapter.(provider.GrowthBuddyOperations); ok {
			status, statusErr := buddy.GrowthBuddyStatus(ctx, raw)
			if statusErr == nil {
				reply.Buddy = &status
				if status.Owned {
					first.ExecutionPending = false
				}
			} else {
				reply.BuddyError = "Buddy 状态暂时无法确认，执行前会重新核对"
			}
		}
	}
	return reply, nil
}

// Manual and action-driven claims share the existing account lock and confirmation.
func (g *Growth) ClaimReward(ctx context.Context, a *accounts.Account, adapter provider.Adapter, raw json.RawMessage, tasks []provider.GrowthTask, code string, ops GrowthOperations) (GrowthResult, error) {
	growth := adapter.(provider.GrowthTasks)
	reply := GrowthResult{OK: true, ID: a.ID, Tasks: tasks}
	reward, claimErr := growth.ClaimGrowthReward(ctx, raw, code)
	g.invalidate(a.ID)
	if claimErr != nil {
		// Never retry a write whose outcome may already have reached upstream.
		return reply, fmt.Errorf("成长奖励结果未确认，请刷新任务后核对；%w", claimErr)
	}
	reply.State, reply.Message = "pending", "领取已提交，等待上游确认；请刷新任务进度"
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return reply, nil
			case <-time.After(300 * time.Millisecond):
			}
		}
		updated, err := growth.ListGrowthTasks(ctx, raw)
		if err != nil {
			break
		}
		reply.Tasks = updated
		if task := GrowthTask(updated, code); task != nil && task.Claimed {
			reply.State, reply.Message, reply.Reward = "claimed", "成长奖励已由上游确认", &reward
			if reward.AlreadyClaimed {
				reply.State, reply.Message = "already_claimed", "该任务奖励此前已领取"
			}
			balanceCtx, balanceCancel := context.WithTimeout(ctx, 3*time.Second)
			reply.Balance, err = ops.Credit(balanceCtx, a, true)
			balanceCancel()
			if err != nil {
				reply.BalanceError = "奖励已确认，积分余额暂时刷新失败"
			}
			break
		}
	}
	return reply, nil
}

func (g *Growth) Claim(ctx context.Context, a *accounts.Account, adapter provider.Adapter, raw json.RawMessage, code string, ops GrowthOperations) (GrowthResult, error) {
	growth := adapter.(provider.GrowthTasks)
	tasks, err := growth.ListGrowthTasks(ctx, raw)
	if err != nil {
		return GrowthResult{}, err
	}
	task := GrowthTask(tasks, code)
	if task == nil {
		return GrowthResult{}, &GrowthRejection{Status: 404, Message: "该账号没有此成长任务"}
	}
	reply := GrowthResult{OK: true, ID: a.ID, Tasks: tasks}
	if task.Claimed {
		reply.State, reply.Message = "already_claimed", "该任务奖励已领取"
		return reply, nil
	}
	if !task.Claimable {
		return GrowthResult{}, &GrowthRejection{Status: 409, Message: "任务尚未达标或尚未解锁，请刷新任务进度"}
	}
	reply, err = g.ClaimReward(ctx, a, adapter, raw, tasks, task.Code, ops)
	if err != nil {
		return GrowthResult{}, err
	}
	return reply, nil
}
