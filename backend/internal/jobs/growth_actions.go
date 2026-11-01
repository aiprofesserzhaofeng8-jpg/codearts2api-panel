package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

func enrolledGrowth(t *provider.GrowthTask) bool {
	return t != nil && (t.Claimed || t.AcceptStatus == "accepted" || t.AcceptStatus == "completed")
}
func (g *Growth) waitGrowth(ctx context.Context, tasksProvider provider.GrowthTasks, raw json.RawMessage, match func([]provider.GrowthTask) bool) ([]provider.GrowthTask, error) {
	var tasks []provider.GrowthTask
	for i := 0; i < 3; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return tasks, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		var err error
		tasks, err = tasksProvider.ListGrowthTasks(ctx, raw)
		if err != nil {
			return tasks, err
		}
		if match(tasks) {
			break
		}
	}
	return tasks, nil
}
func (g *Growth) Accept(ctx context.Context, a *accounts.Account, adapter provider.Adapter, raw json.RawMessage, requested []string) (GrowthResult, error) {
	actions, ok := adapter.(provider.GrowthActions)
	if !ok {
		return GrowthResult{}, &GrowthRejection{Status: 400, Message: "当前上游不支持接取成长任务"}
	}
	growth := adapter.(provider.GrowthTasks)
	tasks, err := growth.ListGrowthTasks(ctx, raw)
	if err != nil {
		return GrowthResult{}, err
	}
	var codes []string
	for _, code := range requested {
		task := GrowthTask(tasks, code)
		if task == nil {
			return GrowthResult{}, &GrowthRejection{Status: 404, Message: "该账号没有此成长任务"}
		}
		if task.Locked {
			return GrowthResult{}, &GrowthRejection{Status: 409, Message: "任务尚未解锁"}
		}
		if task.AcceptNotRequired {
			return GrowthResult{}, &GrowthRejection{Status: 409, Message: "该任务无需接取，请直接点击「解锁并领养」"}
		}
		if !enrolledGrowth(task) {
			codes = append(codes, code)
		}
	}
	reply := GrowthResult{OK: true, ID: a.ID, Tasks: tasks, State: "accepted", Message: "任务已接取"}
	if len(codes) > 0 {
		if err = actions.AcceptGrowthTasks(ctx, raw, codes); err != nil {
			var rejected *provider.Error
			if errors.As(err, &rejected) && rejected.Status == 409 {
				return GrowthResult{}, &GrowthRejection{Status: 409, Message: rejected.Error()}
			}
			return GrowthResult{}, errors.New("接取结果未确认，请刷新核对；" + err.Error())
		}
		updated, readErr := g.waitGrowth(ctx, growth, raw, func(ts []provider.GrowthTask) bool {
			for _, code := range codes {
				if !enrolledGrowth(GrowthTask(ts, code)) {
					return false
				}
			}
			return true
		})
		if readErr == nil {
			reply.Tasks = updated
		}
		for _, code := range codes {
			if readErr != nil || !enrolledGrowth(GrowthTask(reply.Tasks, code)) {
				reply.State, reply.Message = "pending", "接取已提交，等待上游确认；请刷新进度"
				break
			}
		}
	}
	g.Decorate(a.ID, reply.Tasks)
	return reply, nil
}

// GrowthRun keeps the original global slot until the HTTP owner finishes writing
// its response. Close must be called once, including on precondition failures.
type GrowthRun struct {
	growth  *Growth
	adapter provider.Adapter
	actions provider.GrowthActions
	action  provider.GrowthAction
	code    string
	release func()
}

func (g *Growth) BeginRun(adapter provider.Adapter, code string) (*GrowthRun, error) {
	actions, ok := adapter.(provider.GrowthActions)
	if !ok {
		return nil, &GrowthRejection{Status: 400, Message: "当前上游不支持执行成长任务"}
	}
	action, supported := actions.GrowthAction(code)
	if !supported {
		return nil, &GrowthRejection{Status: 409, Message: "该任务需要按说明在官方应用实际完成"}
	}
	slot, ok := g.execution.slot()
	if !ok {
		return nil, &GrowthRejection{Status: 409, Message: "已有两个成长执行任务在运行，请稍后再试"}
	}
	return &GrowthRun{growth: g, adapter: adapter, actions: actions, action: action, code: code, release: slot}, nil
}
func (r *GrowthRun) Close() { r.release() }
func (r *GrowthRun) Run(ctx context.Context, a *accounts.Account, raw json.RawMessage, consent bool, ops GrowthOperations) (GrowthResult, error) {
	g, adapter, actions, action, code := r.growth, r.adapter, r.actions, r.action, r.code
	growth := adapter.(provider.GrowthTasks)
	tasks, err := growth.ListGrowthTasks(ctx, raw)
	if err != nil {
		return GrowthResult{}, err
	}
	task := GrowthTask(tasks, code)
	if task == nil {
		return GrowthResult{}, &GrowthRejection{Status: 404, Message: "该账号没有此成长任务"}
	}
	if task.Locked {
		return GrowthResult{}, &GrowthRejection{Status: 409, Message: "任务尚未解锁"}
	}
	reply := GrowthResult{OK: true, ID: a.ID, Tasks: tasks, State: "pending", Message: "任务进度待确认，请刷新核对"}
	calls, preparationCalls := 0, 0
	finish := func() GrowthResult {
		reply.CompletedCalls = calls + preparationCalls
		g.Decorate(a.ID, reply.Tasks)
		if reply.Buddy != nil && reply.Buddy.Owned {
			if first := GrowthTask(reply.Tasks, "first_buddy"); first != nil {
				first.ExecutionPending = false
			}
		}
		if reply.CompletedCalls > 0 && reply.Balance == nil && ctx.Err() == nil {
			balanceCtx, balanceCancel := context.WithTimeout(ctx, 3*time.Second)
			reply.Balance, err = ops.Credit(balanceCtx, a, true)
			balanceCancel()
			if err != nil {
				reply.BalanceError = "真实对话已产生消耗，积分余额暂时刷新失败"
			}
		}
		return reply
	}
	claim := func() GrowthResult {
		if task.Claimed {
			reply.State, reply.Message = "already_claimed", "该任务奖励已领取"
			return finish()
		}
		var claimErr error
		buddy, reports := reply.Buddy, reply.CompletedReports
		reply, claimErr = g.ClaimReward(ctx, a, adapter, raw, reply.Tasks, code, ops)
		reply.Buddy, reply.CompletedReports = buddy, reports
		if claimErr != nil {
			reply.State, reply.Message = "pending", claimErr.Error()
		}
		return finish()
	}
	if task.Claimed || task.Claimable {
		if code != "first_buddy" {
			return claim(), nil
		}
	}
	if code == "first_buddy" || action.Prerequisite == "first_buddy" && GrowthTask(tasks, "first_buddy") != nil && !GrowthTask(tasks, "first_buddy").Claimed {
		prepared, ready, preparationErr := g.prepareBuddy(ctx, a, adapter, raw, tasks, consent, ops)
		reply, preparationCalls = prepared, prepared.CompletedCalls
		if preparationErr != nil {
			var rejected *provider.Error
			if preparationCalls == 0 && reply.CompletedReports == 0 && errors.As(preparationErr, &rejected) && rejected.Status >= 400 && rejected.Status < 500 {
				return GrowthResult{}, &GrowthRejection{Status: rejected.Status, Message: rejected.Error()}
			}
			reply.Message = preparationErr.Error()
			return finish(), nil
		}
		if code == "first_buddy" || !ready {
			return finish(), nil
		}
		task = GrowthTask(reply.Tasks, code)
		if task == nil || task.Locked {
			reply.State, reply.Message = "pending", "前置已完成，但目标任务状态需要刷新核对"
			return finish(), nil
		}
		if task.Claimed || task.Claimable {
			return claim(), nil
		}
		reply.State, reply.Message = "pending", "领养前置已完成，目标任务进度待确认"
	}
	if action.MaxCalls > 0 && !ops.AllowCreditModels() {
		return GrowthResult{}, &GrowthRejection{Status: 403, Message: "积分模型调用已关闭，未开始真实对话"}
	}
	if !enrolledGrowth(task) {
		if err = actions.AcceptGrowthTasks(ctx, raw, []string{code}); err != nil {
			var rejected *provider.Error
			if errors.As(err, &rejected) && rejected.Status == 409 {
				return GrowthResult{}, &GrowthRejection{Status: 409, Message: rejected.Error()}
			}
			reply.Message = "接取结果未确认，请刷新核对；" + err.Error()
			return finish(), nil
		}
		updated, readErr := g.waitGrowth(ctx, growth, raw, func(ts []provider.GrowthTask) bool { return enrolledGrowth(GrowthTask(ts, code)) })
		if readErr != nil {
			reply.Message = "接取后进度读取失败，请刷新核对"
			return finish(), nil
		}
		reply.Tasks = updated
		task = GrowthTask(updated, code)
		if !enrolledGrowth(task) {
			reply.Message = "接取尚未由上游确认，未开始后续操作"
			return finish(), nil
		}
		if task.Claimed || task.Claimable {
			return claim(), nil
		}
	}
	limit := action.MaxCalls
	if action.MaxReports > 0 {
		limit = action.MaxReports
	}
	if task.Target <= 0 || task.Target-task.Current > limit {
		return GrowthResult{}, &GrowthRejection{Status: 409, Message: "上游任务目标缺失或超出本次操作上限，未开始执行"}
	}
	if action.MaxReports > 0 {
		var ready bool
		reply, ready = g.RunActivity(ctx, adapter, raw, *task, action.MaxReports, reply)
		if ready {
			task = GrowthTask(reply.Tasks, code)
			return claim(), nil
		} else {
			return finish(), nil
		}
	}
	modelID := provider.ModelID(provider.WorkBuddy, "cn", action.Model)
	if ok := ops.ResolveModel(modelID); !ok && !action.NativeModel {
		return GrowthResult{}, &GrowthRejection{Status: 409, Message: "任务指定模型当前不可用，未开始付费对话"}
	}
	for int64(calls) < action.MaxCalls && task.Current < task.Target {
		if calls > 0 {
			if err = growthPause(ctx, 6*time.Second); err != nil {
				reply.Message = "执行已停止，请刷新核对已完成的对话"
				return finish(), nil
			}
		}
		pending, stateErr := g.Pending(a.ID, *task)
		if stateErr != nil {
			reply.Message = stateErr.Error()
			return finish(), nil
		}
		if pending {
			reply.Message = "上次真实对话的进度尚未确认，不会重复调用；请稍后刷新或在官方应用核对"
			return finish(), nil
		}
		previous := task.Current
		completed, runErr := ops.PaidChat(ctx, a, actions, raw, *task, modelID)
		if completed {
			calls++
		}
		if runErr != nil {
			reply.Message = runErr.Error()
			return finish(), nil
		}
		updated, readErr := g.waitGrowthScoring(ctx, growth, raw, func(ts []provider.GrowthTask) bool {
			t := GrowthTask(ts, code)
			return t != nil && (t.Claimed || t.Claimable || t.Current > previous)
		})
		if readErr != nil {
			reply.Message = "真实对话已执行，进度读取失败；不会自动重做"
			return finish(), nil
		}
		reply.Tasks = updated
		task = GrowthTask(updated, code)
		if task == nil || task.Locked || task.Current <= previous && !task.Claimable && !task.Claimed {
			reply.Message = "真实对话已执行，进度尚未确认；不会重复调用，请稍后刷新"
			return finish(), nil
		}
		if task.Claimed || task.Claimable {
			return claim(), nil
		}
	}
	reply.Message = "已达到本次真实对话次数上限，请刷新核对任务进度"
	return finish(), nil
}
