package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/jobs"
	"codearts2api/internal/provider"
)

// Growth progress is authoritative upstream state. Listing/claiming never use
// daily markers or inference accounting. Real chat actions account separately.
type growthReply = jobs.GrowthResult

func (s *Server) growthAccount(w http.ResponseWriter, id string, claim bool) *accounts.Account {
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
	reply, err := s.Growth.List(ctx, a.ID, adapter, raw)
	if err != nil {
		s.writeGrowthError(w, err)
		return
	}
	writeJSON(w, 200, reply)
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
	release, err := s.Growth.Acquire(ctx, body.ID)
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
	reply, err := s.Growth.Claim(ctx, a, adapter, raw, body.TaskCode, s.growthOperations())
	if err != nil {
		s.writeGrowthError(w, err)
		return
	}
	writeJSON(w, 200, reply)
}
