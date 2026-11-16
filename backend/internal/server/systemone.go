package server

import (
	"codearts2api/internal/apierr"
	"codearts2api/internal/gateway"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// System One is a non-streaming decision API. It uses the existing admission,
// credential coordinator and ledger, with one attempt and no prompt rewriting.
func (s *Server) handleSystemOne(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeErr(w, 405, "仅支持 POST", "invalid_request_error")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		writeErr(w, 413, "决策请求超过限制或读取失败", "invalid_request_error")
		return
	}
	if s.Providers == nil {
		writeErr(w, 503, "TypeSafe 未加载", "server_error")
		return
	}
	adapter, ok := s.Providers.Get(provider.TypeSafe)
	if !ok {
		writeErr(w, 503, "TypeSafe 未加载", "server_error")
		return
	}
	decision, ok := adapter.(provider.Decision)
	if !ok {
		writeErr(w, 503, "原生决策接口不可用", "server_error")
		return
	}
	// Validate unmodified bytes first, so duplicate model keys cannot select an
	// account under one name and execute another. Public names remain native here.
	model, err := decision.ValidateDecision(body)
	if err != nil {
		writeErr(w, 400, err.Error(), "invalid_request_error")
		return
	}
	if strings.Contains(model, "/") {
		writeErr(w, 400, "请使用原生决策型号", "invalid_request_error")
		return
	}
	settings := s.Store.Settings()
	if !settings.AllowCreditModels {
		writeErr(w, 403, "上游计费调用已关闭", "permission_error")
		return
	}
	modelID := provider.ModelID(provider.TypeSafe, "global", model)
	r = beginInference(w, r, "systemone")
	if capture, ok := r.Context().Value(usageContextKey{}).(*usageCapture); ok {
		capture.requestedModel = model
	}
	ctx := withRoutingModel(r.Context(), modelID)
	if quote, ok := ctx.Value(consumerRequestKey{}).(*consumerRequest); ok {
		quote.inputBytes = len(body)
	}
	timeout := settings.RequestTimeout
	if timeout <= 0 {
		timeout = 120
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	start := time.Now()
	key := extractKey(r)
	selected, err := s.Admission.Acquire(ctx, gateway.AdmissionRequest{Model: modelID, Key: groupKey(ctx)}, func(ctx context.Context, model string) error {
		if err := s.Routing.CheckNative(groupKey(ctx), model); err != nil {
			return err
		}
		if err := s.Admission.CheckProgress(key, model, requestAttemptDetails(ctx)); err != nil {
			return err
		}
		if quote, ok := ctx.Value(consumerRequestKey{}).(*consumerRequest); ok {
			return s.Admission.Quote(key, quote.id, model, quote.inputBytes, requestAttemptDetails(ctx))
		}
		return nil
	})
	if err != nil {
		e := apierr.From(err)
		s.record(ctx, key, nil, modelID, false, e.Status, start, 0, 0, err)
		writeErr(w, e.Status, err.Error(), e.OpenAI)
		return
	}
	if selected.Release != nil {
		defer selected.Release()
	}
	if selected.Account == nil {
		writeErr(w, 503, "暂无可用 TypeSafe 账号", "server_error")
		return
	}
	acct := selected.Account
	startAttempt(ctx, acct, modelID)
	_, raw, err := s.providerCredentials(ctx, acct)
	var result *provider.DecisionResult
	if err == nil {
		result, err = decision.Decide(ctx, raw, body)
	}
	if err == nil && result != nil && result.Usage.Known {
		usage, _ := json.Marshal(map[string]any{"usage": map[string]int64{"prompt_tokens": result.Usage.InputTokens, "completion_tokens": result.Usage.OutputTokens}})
		captureUsage(ctx, usage)
	}
	if err == nil && result == nil {
		err = providerhttp.Broken("TypeSafe")
	}
	finishAttempt(ctx, err)
	if err != nil {
		e := apierr.From(err)
		s.record(ctx, key, acct, modelID, false, e.Status, start, 0, 0, err)
		writeErr(w, e.Status, err.Error(), e.OpenAI)
		return
	}
	s.record(ctx, key, acct, modelID, false, 200, start, 0, 0, nil)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(result.Body)
}
