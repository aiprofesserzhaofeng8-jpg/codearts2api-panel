package catalog

import (
	"codearts2api/internal/provider"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ProbeMaxModels 是一次测活请求允许探测的模型数上限。
//
// 每个模型都是一次真实上游请求、真实扣积分，因此必须有上限：上游清单异常变大时，
// 不能让一次点击变成无界的消耗。超限要求调用方分批，而不是静默截断。
const ProbeMaxModels = 64

// probeAttemptTimeout 是「一个模型 × 一个账号」的探测超时。
//
// 上游正常几百毫秒到几秒就返回；30 秒还没回基本可判定这次尝试无望，
// 早点换下一个账号，避免「多账号 × 多模型」把整个请求拖到客户端超时。
const probeAttemptTimeout = 30 * time.Second

// A probe run holds the original independent lock across HTTP decoding and work.
// Only the caller that successfully begins a run may close it, exactly once.
type ProbeRun struct{ runtime *Runtime }

var ErrProbeBusy = errors.New("已有测活任务在运行，请等它结束后再试")

func (s *Runtime) BeginProbe() (*ProbeRun, error) {
	if !s.probeMu.TryLock() {
		return nil, ErrProbeBusy
	}
	return &ProbeRun{runtime: s}, nil
}
func (p *ProbeRun) Close() { p.runtime.probeMu.Unlock() }

type ProbeResult struct {
	Total     int
	Available int
	Results   []map[string]any
}

func (p *ProbeRun) Run(ctx context.Context, id string, models []string, ops ProbeOperations) (ProbeResult, error) {
	s := p.runtime
	targets := s.store.Accounts()
	if id != "" {
		targets = nil
		if a := s.store.Account(id); a != nil {
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		return ProbeResult{}, errors.New("没有可用账号")
	}

	// 未指定模型时探测当前清单里的全部模型。
	want := models
	if len(want) == 0 {
		st := s.store.Settings()
		for _, m := range s.directory.List(st.ModelAliases) {
			// 别名与本体重复探测没有意义，只探上游真实 ID。
			if m.Source == "alias" {
				continue
			}
			want = append(want, m.ID)
		}
	}
	want = dedupeModels(want)
	if len(want) == 0 {
		return ProbeResult{}, errors.New("模型清单为空")
	}
	if len(want) > ProbeMaxModels {
		return ProbeResult{}, fmt.Errorf("一次最多测活 %d 个模型（当前 %d 个），请分批进行", ProbeMaxModels, len(want))
	}

	// 并发上限按模型数收敛：每个探测都是一次真实上游请求，串行太慢、
	// 全并发又会撞单账号 3 会话的限制。
	limit := 4
	results := make([]map[string]any, len(want))
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, modelID := range want {
		wg.Add(1)
		go func(idx int, modelID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			row := map[string]any{"model": modelID}
			// 依次尝试账号，任一账号能用即判定该模型可用。
			for _, a := range targets {
				upstream, scope, _ := provider.ParseModel(modelID)
				if a.ProviderID() != upstream || (upstream != provider.CodeArts && a.Scope != scope) {
					continue
				}
				if upstream != provider.CodeArts {
					ctx, cancel := context.WithTimeout(ctx, probeAttemptTimeout)
					model, ok := s.directory.Resolve(modelID, nil)
					var err error
					if !ok {
						err = fmt.Errorf("模型不存在")
					} else {
						body, _ := json.Marshal(map[string]any{"model": modelID, "messages": []map[string]string{{"role": "user", "content": "Reply OK."}}, "max_tokens": 64})
						_, err = ops.Provider(ctx, a, model, body)
					}
					cancel()
					row["ok"] = err == nil
					row["account"] = a.DisplayName()
					if err != nil {
						row["error"] = err.Error()
						continue
					}
					break
				}
				// 客户端断开后不必再花积分试下一个账号。
				if ctx.Err() != nil {
					row["ok"] = false
					row["error"] = "测活已取消"
					break
				}
				ctx, cancel := context.WithTimeout(ctx, probeAttemptTimeout)
				err := ops.Native(ctx, a, modelID)
				cancel()
				if err == nil {
					row["ok"] = true
					row["account"] = a.DisplayName()
					break
				}
				row["ok"] = false
				row["error"] = err.Error()
				row["account"] = a.DisplayName()
			}
			results[idx] = row
		}(i, modelID)
	}
	wg.Wait()

	okCount := 0
	for _, row := range results {
		if row["ok"] == true {
			okCount++
		}
	}
	return ProbeResult{Total: len(results), Available: okCount, Results: results}, nil
}

// dedupeModels 去掉空值、首尾空白与重复的模型 ID。
//
// 重复 ID 会变成两次真实扣费，而调用方（控制台）没有任何理由需要重复探测。
func dedupeModels(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
