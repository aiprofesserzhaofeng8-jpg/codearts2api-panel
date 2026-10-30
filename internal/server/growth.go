package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
)

// Growth progress is authoritative upstream state. It never uses daily claim
// markers, inference health, key budgets, or the usage settlement journal.
type growthReply struct {
	OK           bool                   `json:"ok"`
	ID           string                 `json:"id"`
	Tasks        []provider.GrowthTask  `json:"tasks"`
	State        string                 `json:"state,omitempty"`
	Message      string                 `json:"message,omitempty"`
	Reward       *provider.GrowthReward `json:"reward,omitempty"`
	Balance      *codearts.CreditInfo   `json:"balance,omitempty"`
	BalanceError string                 `json:"balance_error,omitempty"`
}

func (s *Server) growthAccount(w http.ResponseWriter, id string, claim bool) *codearts.Account {
	if id == "" {
		writeErr(w, 400, "请指定成长任务账号", "invalid_request_error")
		return nil
	}
	a := s.Store.Account(id)
	if a == nil {
		writeErr(w, 404, "账号不存在", "invalid_request_error")
		return nil
	}
	if s.Providers != nil && a.ProviderID() == provider.WorkBuddy {
		if adapter, ok := s.Providers.Get(a.ProviderID()); ok {
			if growth, ok := adapter.(provider.GrowthTasks); ok && growth.SupportsGrowthTasks(a.Credentials) {
				if claim && !a.Enabled {
					writeErr(w, 409, "账号已停用，请启用后再领取", "invalid_request_error")
					return nil
				}
				return a
			}
		}
	}
	writeErr(w, 400, "成长任务仅支持 WorkBuddy 国内个人账号", "invalid_request_error")
	return nil
}

func (s *Server) handleGrowthTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	a := s.growthAccount(w, strings.TrimSpace(r.URL.Query().Get("id")), false)
	if a == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	adapter, raw, err := s.providerCredentials(ctx, a)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	tasks, err := adapter.(provider.GrowthTasks).ListGrowthTasks(ctx, raw)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	writeJSON(w, 200, growthReply{OK: true, ID: a.ID, Tasks: tasks})
}

func growthTask(tasks []provider.GrowthTask, code string) *provider.GrowthTask {
	for i := range tasks {
		if tasks[i].Code == code {
			return &tasks[i]
		}
	}
	return nil
}

func (s *Server) handleGrowthClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	var body struct {
		ID       string `json:"id"`
		TaskCode string `json:"task_code"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF || !provider.ValidGrowthTaskCode(body.TaskCode) {
		writeErr(w, 400, "账号或成长任务参数无效", "invalid_request_error")
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	if s.growthAccount(w, body.ID, true) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// Only task claims for this account serialize. Never hold credential/store
	// locks while doing RPC or waiting for upstream accounting to catch up.
	release, err := s.growthRuntime.lock(ctx, body.ID)
	if err != nil {
		writeErr(w, 408, "等待成长任务操作超时", "upstream_error")
		return
	}
	defer release()
	a := s.growthAccount(w, strings.TrimSpace(body.ID), true)
	if a == nil {
		return
	}
	adapter, raw, err := s.providerCredentials(ctx, a)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	growth := adapter.(provider.GrowthTasks)
	tasks, err := growth.ListGrowthTasks(ctx, raw)
	if err != nil {
		writeErr(w, 502, err.Error(), "upstream_error")
		return
	}
	task := growthTask(tasks, body.TaskCode)
	if task == nil {
		writeErr(w, 404, "该账号没有此成长任务", "invalid_request_error")
		return
	}
	reply := growthReply{OK: true, ID: a.ID, Tasks: tasks}
	if task.Claimed {
		reply.State, reply.Message = "already_claimed", "该任务奖励已领取"
		writeJSON(w, 200, reply)
		return
	}
	if !task.Claimable {
		writeErr(w, 409, "任务尚未达标或尚未解锁，请刷新任务进度", "invalid_request_error")
		return
	}
	reward, claimErr := growth.ClaimGrowthReward(ctx, raw, task.Code)
	s.creditMu.Lock()
	delete(s.creditCache, a.ID)
	s.creditMu.Unlock()
	if claimErr != nil {
		// Never retry a write whose outcome may already have reached upstream.
		writeErr(w, 502, "成长奖励结果未确认，请刷新任务后核对；"+claimErr.Error(), "upstream_error")
		return
	}
	reply.State, reply.Message = "pending", "领取已提交，等待上游确认；请刷新任务进度"
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				writeJSON(w, 200, reply)
				return
			case <-time.After(300 * time.Millisecond):
			}
		}
		updated, err := growth.ListGrowthTasks(ctx, raw)
		if err != nil {
			break
		}
		reply.Tasks = updated
		if task := growthTask(updated, body.TaskCode); task != nil && task.Claimed {
			reply.State, reply.Message, reply.Reward = "claimed", "成长奖励已由上游确认", &reward
			if reward.AlreadyClaimed {
				reply.State, reply.Message = "already_claimed", "该任务奖励此前已领取"
			}
			balanceCtx, balanceCancel := context.WithTimeout(ctx, 3*time.Second)
			reply.Balance, err = s.creditFor(balanceCtx, a, true)
			balanceCancel()
			if err != nil {
				reply.BalanceError = "奖励已确认，积分余额暂时刷新失败"
			}
			break
		}
	}
	writeJSON(w, 200, reply)
}
