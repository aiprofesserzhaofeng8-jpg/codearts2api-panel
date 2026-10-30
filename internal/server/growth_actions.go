package server

import (
	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func decodeGrowthBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		writeErr(w, 400, "成长任务参数无效", "invalid_request_error")
		return false
	}
	return true
}
func enrolledGrowth(t *provider.GrowthTask) bool {
	return t != nil && (t.Claimed || t.AcceptStatus == "accepted" || t.AcceptStatus == "completed")
}
func (s *Server) waitGrowth(ctx context.Context, g provider.GrowthTasks, raw json.RawMessage, match func([]provider.GrowthTask) bool) ([]provider.GrowthTask, error) {
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
		tasks, err = g.ListGrowthTasks(ctx, raw)
		if err != nil {
			return tasks, err
		}
		if match(tasks) {
			break
		}
	}
	return tasks, nil
}
func (s *Server) handleGrowthAccept(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string   `json:"id"`
		Codes []string `json:"task_codes"`
	}
	if !decodeGrowthBody(w, r, &body) {
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	if len(body.Codes) == 0 || len(body.Codes) > 20 {
		writeErr(w, 400, "每次接取 1 至 20 项任务", "invalid_request_error")
		return
	}
	seen := map[string]bool{}
	for _, code := range body.Codes {
		if !provider.ValidGrowthTaskCode(code) || seen[code] {
			writeErr(w, 400, "任务标识无效或重复", "invalid_request_error")
			return
		}
		seen[code] = true
	}
	if s.growthAccount(w, body.ID, true) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	release, err := s.growthRuntime.lock(ctx, body.ID)
	if err != nil {
		writeErr(w, 408, "等待成长任务操作超时", "upstream_error")
		return
	}
	defer release()
	a := s.growthAccount(w, body.ID, true)
	if a == nil {
		return
	}
	adapter, raw, err := s.providerCredentials(ctx, a)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	actions, ok := adapter.(provider.GrowthActions)
	if !ok {
		writeErr(w, 400, "当前上游不支持接取成长任务", "invalid_request_error")
		return
	}
	growth := adapter.(provider.GrowthTasks)
	tasks, err := growth.ListGrowthTasks(ctx, raw)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	var codes []string
	for _, code := range body.Codes {
		task := growthTask(tasks, code)
		if task == nil {
			writeErr(w, 404, "该账号没有此成长任务", "invalid_request_error")
			return
		}
		if task.Locked {
			writeErr(w, 409, "任务尚未解锁", "invalid_request_error")
			return
		}
		if !enrolledGrowth(task) {
			codes = append(codes, code)
		}
	}
	reply := growthReply{OK: true, ID: a.ID, Tasks: tasks, State: "accepted", Message: "任务已接取"}
	if len(codes) > 0 {
		if err = actions.AcceptGrowthTasks(ctx, raw, codes); err != nil {
			writeErr(w, 502, "接取结果未确认，请刷新核对；"+err.Error(), "upstream_error")
			return
		}
		updated, readErr := s.waitGrowth(ctx, growth, raw, func(ts []provider.GrowthTask) bool {
			for _, code := range codes {
				if !enrolledGrowth(growthTask(ts, code)) {
					return false
				}
			}
			return true
		})
		if readErr == nil {
			reply.Tasks = updated
		}
		for _, code := range codes {
			if readErr != nil || !enrolledGrowth(growthTask(reply.Tasks, code)) {
				reply.State, reply.Message = "pending", "接取已提交，等待上游确认；请刷新进度"
				break
			}
		}
	}
	s.decorateGrowthTasks(a.ID, reply.Tasks)
	writeJSON(w, 200, reply)
}

func (s *Server) handleGrowthRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Code string `json:"task_code"`
	}
	if !decodeGrowthBody(w, r, &body) {
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	if !provider.ValidGrowthTaskCode(body.Code) {
		writeErr(w, 400, "任务标识无效", "invalid_request_error")
		return
	}
	if s.growthAccount(w, body.ID, true) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	release, err := s.growthRuntime.lock(ctx, body.ID)
	if err != nil {
		writeErr(w, 408, "等待成长任务操作超时", "upstream_error")
		return
	}
	defer release()
	a := s.growthAccount(w, body.ID, true)
	if a == nil {
		return
	}
	adapter, raw, err := s.providerCredentials(ctx, a)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	actions, ok := adapter.(provider.GrowthActions)
	if !ok {
		writeErr(w, 400, "当前上游不支持执行成长任务", "invalid_request_error")
		return
	}
	action, supported := actions.GrowthAction(body.Code)
	if !supported {
		writeErr(w, 409, "该任务需要按说明在官方应用实际完成", "invalid_request_error")
		return
	}
	slot, ok := s.growthExecution.slot()
	if !ok {
		writeErr(w, 409, "已有两个成长执行任务在运行，请稍后再试", "invalid_request_error")
		return
	}
	defer slot()
	growth := adapter.(provider.GrowthTasks)
	tasks, err := growth.ListGrowthTasks(ctx, raw)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	task := growthTask(tasks, body.Code)
	if task == nil {
		writeErr(w, 404, "该账号没有此成长任务", "invalid_request_error")
		return
	}
	if task.Locked {
		writeErr(w, 409, "任务尚未解锁", "invalid_request_error")
		return
	}
	reply := growthReply{OK: true, ID: a.ID, Tasks: tasks, State: "pending", Message: "任务进度待确认，请刷新核对"}
	calls := 0
	finish := func() {
		reply.CompletedCalls = calls
		s.decorateGrowthTasks(a.ID, reply.Tasks)
		if calls > 0 && reply.Balance == nil && ctx.Err() == nil {
			balanceCtx, balanceCancel := context.WithTimeout(ctx, 3*time.Second)
			reply.Balance, err = s.creditFor(balanceCtx, a, true)
			balanceCancel()
			if err != nil {
				reply.BalanceError = "真实对话已产生消耗，积分余额暂时刷新失败"
			}
		}
		writeJSON(w, 200, reply)
	}
	claim := func() {
		if task.Claimed {
			reply.State, reply.Message = "already_claimed", "该任务奖励已领取"
			finish()
			return
		}
		var claimErr error
		reply, claimErr = s.claimGrowthReward(ctx, a, adapter, raw, reply.Tasks, body.Code)
		if claimErr != nil {
			reply.State, reply.Message = "pending", claimErr.Error()
		}
		finish()
	}
	if task.Claimed || task.Claimable {
		claim()
		return
	}
	if !s.Store.Settings().AllowCreditModels {
		writeErr(w, 403, "积分模型调用已关闭，未开始真实对话", "invalid_request_error")
		return
	}
	if !enrolledGrowth(task) {
		if err = actions.AcceptGrowthTasks(ctx, raw, []string{body.Code}); err != nil {
			reply.Message = "接取结果未确认，请刷新核对；" + err.Error()
			finish()
			return
		}
		updated, readErr := s.waitGrowth(ctx, growth, raw, func(ts []provider.GrowthTask) bool { return enrolledGrowth(growthTask(ts, body.Code)) })
		if readErr != nil {
			reply.Message = "接取后进度读取失败，请刷新核对"
			finish()
			return
		}
		reply.Tasks = updated
		task = growthTask(updated, body.Code)
		if !enrolledGrowth(task) {
			reply.Message = "接取尚未由上游确认，未开始付费对话"
			finish()
			return
		}
		if task.Claimed || task.Claimable {
			claim()
			return
		}
	}
	if task.Target <= 0 || task.Target-task.Current > action.MaxCalls {
		writeErr(w, 409, "上游任务目标缺失或超出执行次数上限，未开始付费对话", "invalid_request_error")
		return
	}
	modelID := provider.ModelID(provider.WorkBuddy, "cn", action.Model)
	if _, ok = s.Reg.Resolve(modelID, nil); !ok {
		writeErr(w, 409, "任务指定模型当前不可用，未开始付费对话", "invalid_request_error")
		return
	}
	for int64(calls) < action.MaxCalls && task.Current < task.Target {
		pending, stateErr := s.growthExecution.pending(s.growthExecutionPath(), a.ID, *task)
		if stateErr != nil {
			reply.Message = stateErr.Error()
			finish()
			return
		}
		if pending {
			reply.Message = "上次真实对话的进度尚未确认，不会重复调用；请稍后刷新或在官方应用核对"
			finish()
			return
		}
		previous := task.Current
		completed, runErr := s.runGrowthChat(ctx, a, actions, raw, *task, modelID)
		if completed {
			calls++
		}
		if runErr != nil {
			reply.Message = runErr.Error()
			finish()
			return
		}
		updated, readErr := s.waitGrowth(ctx, growth, raw, func(ts []provider.GrowthTask) bool {
			t := growthTask(ts, body.Code)
			return t != nil && (t.Claimed || t.Claimable || t.Current > previous)
		})
		if readErr != nil {
			reply.Message = "真实对话已执行，进度读取失败；不会自动重做"
			finish()
			return
		}
		reply.Tasks = updated
		task = growthTask(updated, body.Code)
		if task == nil || task.Locked || task.Current <= previous && !task.Claimable && !task.Claimed {
			reply.Message = "真实对话已执行，进度尚未确认；不会重复调用，请稍后刷新"
			finish()
			return
		}
		if task.Claimed || task.Claimable {
			claim()
			return
		}
	}
	reply.Message = "已达到本次真实对话次数上限，请刷新核对任务进度"
	finish()
}

func (s *Server) runGrowthChat(ctx context.Context, a *codearts.Account, actions provider.GrowthActions, raw json.RawMessage, task provider.GrowthTask, model string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	settings := s.Store.Settings()
	if !settings.AllowCreditModels {
		return false, fmt.Errorf("积分模型调用已关闭，已停止后续对话")
	}
	timeout := time.Duration(settings.RequestTimeout) * time.Second
	if timeout <= 0 || timeout > 45*time.Second {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	budget, err := s.Store.BeginKeyBudget("")
	if err != nil {
		return false, fmt.Errorf("用量账本不可用，未开始付费对话")
	}
	defer budget()
	exclude := map[string]bool{}
	for _, other := range s.Store.Accounts() {
		if other.ID != a.ID {
			exclude[other.ID] = true
		}
	}
	leased, release := s.Store.AcquireAccountForModel("", model, exclude)
	defer release()
	if leased == nil || leased.ID != a.ID {
		return false, fmt.Errorf("该账号繁忙、暂停或冷却中，未开始付费对话")
	}
	if err = s.growthExecution.checkpoint(s.growthExecutionPath(), a.ID, task, false); err != nil {
		return false, err
	}
	chatCtx := withUsageCapture(ctx, "growth")
	capture := chatCtx.Value(usageContextKey{}).(*usageCapture)
	capture.kind = "growth"
	capture.stream = true
	capture.started = time.Now()
	startAttempt(chatCtx, a, model)
	out := 0
	completed, runErr := actions.RunGrowthChat(chatCtx, raw, task.Code, func(b []byte) error { captureUsage(chatCtx, b); out += deltaLen(b); return nil })
	finishAttempt(chatCtx, runErr)
	status := 200
	if runErr != nil {
		status = 502
	}
	recordErr := s.record(chatCtx, "", a, model, true, status, capture.started, len([]rune("请用一句话介绍你自己。")), out, runErr)
	s.creditMu.Lock()
	delete(s.creditCache, a.ID)
	s.creditMu.Unlock()
	// Explicit pre-processing rejections did not complete a chat. Uncertain
	// network/5xx/stream/report failures retain the checkpoint, including restart.
	var upstream *provider.Error
	if !completed && !capture.attemptObserved && errors.As(runErr, &upstream) && upstream.Status >= 400 && upstream.Status < 500 {
		if clearErr := s.growthExecution.checkpoint(s.growthExecutionPath(), a.ID, task, true); clearErr != nil {
			return completed, clearErr
		}
	}
	if recordErr != nil {
		return completed, fmt.Errorf("用量记录保存失败，已停止后续付费对话")
	}
	return completed, runErr
}
