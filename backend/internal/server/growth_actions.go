package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
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
	release, err := s.Growth.Acquire(ctx, body.ID)
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
	reply, err := s.Growth.Accept(ctx, a, adapter, raw, body.Codes)
	if err != nil {
		s.writeGrowthError(w, err)
		return
	}
	writeJSON(w, 200, reply)
}

func (s *Server) handleGrowthRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string `json:"id"`
		Code       string `json:"task_code"`
		AgreeBuddy bool   `json:"agree_buddy,omitempty"`
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
	release, err := s.Growth.Acquire(ctx, body.ID)
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
	run, err := s.Growth.BeginRun(adapter, body.Code)
	if err != nil {
		s.writeGrowthError(w, err)
		return
	}
	defer run.Close()
	reply, err := run.Run(ctx, a, raw, body.AgreeBuddy, s.growthOperations())
	if err != nil {
		s.writeGrowthError(w, err)
		return
	}
	writeJSON(w, 200, reply)
}

func (s *Server) runGrowthChat(ctx context.Context, a *accounts.Account, actions provider.GrowthActions, raw json.RawMessage, task provider.GrowthTask, model string) (bool, error) {
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
	// Resolve expert identity and submit its non-billable summon chain before
	// opening a paid checkpoint. Ordinary model requests never use this path.
	planner, planned := actions.(provider.GrowthChatSteps)
	var step provider.GrowthChatStep
	if planned {
		var err error
		step, err = planner.PrepareGrowthChat(ctx, raw, task)
		if err != nil {
			return false, err
		}
	}
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
	if err = s.Growth.Checkpoint(a.ID, task, false); err != nil {
		return false, err
	}
	chatCtx := withUsageCapture(ctx, "growth")
	capture := chatCtx.Value(usageContextKey{}).(*usageCapture)
	capture.kind = "growth"
	capture.stream = true
	capture.started = time.Now()
	startAttempt(chatCtx, a, model)
	out := 0
	chunk := func(b []byte) error { captureUsage(chatCtx, b); out += deltaLen(b); return nil }
	var completed bool
	var runErr error
	if planned {
		completed, runErr = planner.RunGrowthChatStep(chatCtx, raw, step, chunk)
	} else {
		completed, runErr = actions.RunGrowthChat(chatCtx, raw, task.Code, chunk)
	}
	finishAttempt(chatCtx, runErr)
	status := 200
	if runErr != nil {
		status = 502
	}
	recordErr := s.record(chatCtx, "", a, model, true, status, capture.started, len([]rune("请用一句话介绍你自己。")), out, runErr)
	s.Credits.Invalidate(a.ID)
	// Explicit pre-processing rejections did not complete a chat. Uncertain
	// network/5xx/stream/report failures retain the checkpoint, including restart.
	var upstream *provider.Error
	if !completed && !capture.attemptObserved && errors.As(runErr, &upstream) && upstream.Status >= 400 && upstream.Status < 500 {
		if clearErr := s.Growth.Checkpoint(a.ID, task, true); clearErr != nil {
			return completed, clearErr
		}
	}
	if recordErr != nil {
		return completed, fmt.Errorf("用量记录保存失败，已停止后续付费对话")
	}
	return completed, runErr
}
