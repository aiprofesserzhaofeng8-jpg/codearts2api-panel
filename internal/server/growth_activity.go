package server

// Activity execution is isolated from billable model execution. Native progress
// is re-read after every report; no unknown result starts another report.
import (
	"context"
	"encoding/json"
	"time"

	"codearts2api/internal/provider"
)

func growthPause(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Server) waitGrowthScoring(ctx context.Context, growth provider.GrowthTasks, raw json.RawMessage, ready func([]provider.GrowthTask) bool) ([]provider.GrowthTask, error) {
	var tasks []provider.GrowthTask
	for poll := 0; poll < 4; poll++ {
		if poll > 0 {
			if err := growthPause(ctx, 3*time.Second); err != nil {
				return tasks, err
			}
		}
		var err error
		tasks, err = growth.ListGrowthTasks(ctx, raw)
		if err != nil || ready(tasks) {
			return tasks, err
		}
	}
	return tasks, nil
}

func (s *Server) runGrowthActivity(ctx context.Context, adapter provider.Adapter, raw json.RawMessage, task provider.GrowthTask, limit int64, reply growthReply) (growthReply, bool) {
	activity, ok := adapter.(provider.GrowthActivityActions)
	if !ok {
		reply.Message = "当前上游不支持活跃上报"
		return reply, false
	}
	growth := adapter.(provider.GrowthTasks)
	for sent := int64(0); sent < limit && task.Current < task.Target; sent++ {
		if sent > 0 {
			if err := growthPause(ctx, 1050*time.Millisecond); err != nil {
				reply.Message = "活跃任务已停止，已提交的上报请刷新核对"
				return reply, false
			}
		}
		if err := activity.ReportGrowthActivity(ctx, raw, task); err != nil {
			reply.Message = "活跃上报未确认，已停止后续操作：" + err.Error()
			return reply, false
		}
		reply.CompletedReports++
		previous, advanced := task.Current, false
		// Reference scoring is asynchronous (often 5–8s). Wait only in this
		// explicit task, returning immediately when native progress advances.
		for poll := 0; poll < 4; poll++ {
			if poll > 0 {
				if err := growthPause(ctx, 3*time.Second); err != nil {
					reply.Message = "等待活跃进度已停止，请刷新核对"
					return reply, false
				}
			}
			updated, err := growth.ListGrowthTasks(ctx, raw)
			if err != nil {
				reply.Message = "活跃上报后进度读取失败，已停止后续操作；请刷新核对"
				return reply, false
			}
			reply.Tasks = updated
			next := growthTask(updated, task.Code)
			if next == nil || next.Locked || next.Target != task.Target {
				reply.Message = "活跃任务状态发生变化，已停止；请刷新核对"
				return reply, false
			}
			task = *next
			if task.Claimed || task.Claimable {
				return reply, true
			}
			if task.Current > previous {
				advanced = true
				break
			}
		}
		if !advanced {
			reply.Message = "活跃上报已提交，进度尚未确认；本轮不重复上报，请稍后刷新"
			return reply, false
		}
	}
	reply.Message = "已达到本次活跃上报上限，请刷新核对任务进度"
	return reply, false
}
