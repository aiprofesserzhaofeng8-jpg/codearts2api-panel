package server

// WorkBuddy onboarding is an explicit task action, independent from account
// routing, daily check-in and other providers. Only verified ownership unlocks
// dependent actions; unknown writes are re-read, never automatically replayed.
import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
)

func (s *Server) prepareGrowthBuddy(ctx context.Context, a *codearts.Account, adapter provider.Adapter, raw json.RawMessage, tasks []provider.GrowthTask, consent bool) (growthReply, bool, error) {
	reply := growthReply{OK: true, ID: a.ID, Tasks: tasks, State: "pending"}
	ops, ok := adapter.(provider.GrowthBuddyOperations)
	if !ok {
		return reply, false, fmt.Errorf("当前上游不支持自动领养 Buddy")
	}
	first := growthTask(tasks, "first_buddy")
	if first == nil || first.Locked {
		return reply, false, &provider.Error{Status: 409, Message: "领养前置任务不存在或尚未解锁"}
	}
	status, err := ops.GrowthBuddyStatus(ctx, raw)
	if err != nil {
		return reply, false, fmt.Errorf("Buddy 状态读取失败，未执行新的动作")
	}
	reply.Buddy = &status
	if !status.Owned {
		if !status.Agreed && !consent {
			return reply, false, &provider.Error{Status: 409, Message: "首次领养需阅读官方活动规则，并勾选同意领养协议后再执行"}
		}
		if !first.Claimed && !first.Claimable {
			activity, supported := adapter.(provider.GrowthActivityActions)
			if !supported {
				return reply, false, fmt.Errorf("当前上游不支持领养前置活跃上报")
			}
			// Activity has no billable model call. Historical uncertain-chat
			// checkpoints remain intact and must not block explicit onboarding.
			if err := activity.ReportGrowthActivity(ctx, raw, "first_buddy"); err != nil {
				return reply, false, fmt.Errorf("领养前置活跃上报未确认：%w", err)
			}
			reply.CompletedReports = 1
			select {
			case <-ctx.Done():
				return reply, false, ctx.Err()
			case <-time.After(1050 * time.Millisecond):
			}
		}
		if err := ctx.Err(); err != nil {
			return reply, false, err
		}
		if !status.Agreed {
			if err = ops.AgreeGrowthBuddy(ctx, raw); err != nil {
				return reply, false, fmt.Errorf("领养协议提交未确认，请刷新核对：%w", err)
			}
			status, err = ops.GrowthBuddyStatus(ctx, raw)
			if err != nil || !status.Owned && !status.Agreed {
				return reply, false, fmt.Errorf("领养协议尚未确认，未提交领养；请刷新核对")
			}
			reply.Buddy = &status
		}
		if !status.Owned {
			adoptErr := ops.AdoptGrowthBuddy(ctx, raw)
			// Even an errored POST can have reached upstream. Confirm ownership
			// with bounded reads rather than replaying an adoption or model call.
			for attempt := 0; attempt < 3; attempt++ {
				if attempt > 0 {
					select {
					case <-ctx.Done():
						return reply, false, ctx.Err()
					case <-time.After(500 * time.Millisecond):
					}
				}
				status, err = ops.GrowthBuddyStatus(ctx, raw)
				if err == nil {
					reply.Buddy = &status
					if status.Owned {
						break
					}
				}
			}
			if err != nil || !status.Owned {
				message := "领养尚未由上游确认，已停止后续任务；请稍后刷新"
				if adoptErr != nil {
					return reply, false, fmt.Errorf("领养未确认：%w", adoptErr)
				}
				return reply, false, fmt.Errorf("%s", message)
			}
		}
	}

	reply.State, reply.Message = "prepared", "Buddy 领养已确认，后续任务可继续；成长奖励以任务进度为准"
	updated, readErr := adapter.(provider.GrowthTasks).ListGrowthTasks(ctx, raw)
	if readErr != nil {
		reply.Message = "Buddy 领养已确认，任务进度暂时读取失败，请刷新核对奖励"
		return reply, true, nil
	}
	reply.Tasks = updated
	first = growthTask(updated, "first_buddy")
	if first != nil && first.Claimed {
		reply.State, reply.Message = "already_claimed", "Buddy 领养及成长奖励均已确认"
	} else if first != nil && first.Claimable {
		claimed, claimErr := s.claimGrowthReward(ctx, a, adapter, raw, updated, "first_buddy")
		if claimErr == nil {
			claimed.Buddy, claimed.CompletedReports = reply.Buddy, reply.CompletedReports
			return claimed, true, nil
		}
		reply.Message = "Buddy 领养已确认，奖励领取结果待刷新核对"
	}
	return reply, true, nil
}
