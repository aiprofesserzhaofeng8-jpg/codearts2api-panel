package server

import (
	"codearts2api/internal/apierr"
	"codearts2api/internal/config"
	"codearts2api/internal/storage"
	"encoding/json"
	"errors"
	"net/http"
)

type summaryIntentKey struct{}

func profileFromPolicy(model string, p storage.ModelPolicy) map[string]any {
	return map[string]any{"version": 1, "model": model, "context_window": p.ContextWindow, "max_output_tokens": p.MaxOutputTokens,
		"context_basis": p.Source, "compaction": map[string]int{"reserve_tokens": p.ReserveTokens, "keep_recent_tokens": p.KeepRecentTokens, "trigger_tokens": p.TriggerTokens},
		"protocols": []string{"chat_completions", "responses", "anthropic_messages"}, "pi_summary_adapter": p.SummaryEnabled,
		"claude_code":       map[string]bool{"thinking_control": false, "effort_control": false},
		"summary":           map[string]any{"enabled": p.SummaryEnabled, "bounded_retry": p.SummaryRetry, "max_attempts": 1 + boolInt(p.SummaryRetry), "max_tokens": p.SummaryMaxTokens, "buffer_until_complete": true, "fallback_model": p.SummaryFallbackModel},
		"upstream_protocol": "chat_completions", "responses_native": false, "responses_websocket": false, "responses_store": false, "responses_compact": false}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Kept as a pure default helper for callers/tests; live clients use Store policies.
func clientProfile(model string) map[string]any {
	p, ok := storage.DefaultModelPolicies()[model]
	if !ok {
		return nil
	}
	return profileFromPolicy(model, p)
}
func (s *Server) handleClientProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	requested := r.URL.Query().Get("model")
	if requested == "" {
		requested = s.Routing.ClientDefault(groupKey(r.Context()))
	}
	model, err := s.resolveModelForRequest(r, s.Store.Settings(), requested)
	if err != nil {
		writeErr(w, apierr.From(err).Status, err.Error(), "invalid_request_error")
		return
	}
	p, ok := s.requestModelPolicy(r, model)
	if !ok {
		writeErr(w, 422, "该模型暂无客户端推荐参数，请在模型页配置并验证", "invalid_request_error")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, profileFromPolicy(model.ID, p))
}
func (s *Server) handleAdminModelPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"policies": s.ModelPolicies.All(), "capabilities": gatewayCapabilities()})
	case http.MethodPost:
		var body struct {
			Model  string             `json:"model"`
			Policy config.ModelPolicy `json:"policy"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
			writeErr(w, 400, "配置格式无效", "invalid_request_error")
			return
		}
		if err := s.Configuration.SaveModelPolicy(body.Model, body.Policy); err != nil {
			if errors.Is(err, config.ErrModelPolicySave) {
				writeErr(w, 500, "保存模型策略失败", "server_error")
			} else {
				writeErr(w, 400, err.Error(), "invalid_request_error")
			}
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "policies": s.ModelPolicies.All()})
	default:
		writeErr(w, 405, "仅支持 GET/POST", "invalid_request_error")
	}
}
func gatewayCapabilities() map[string]any {
	return map[string]any{
		"upstream":         map[string]any{"provider": "codearts", "protocol": "chat_completions", "endpoint": "/api/v2/chat/completions", "native_responses": "not_observed", "quantization": "unknown", "maximum_context": "unknown"},
		"client_protocols": map[string]string{"chat_completions": "forwarded", "responses": "translated", "anthropic_messages": "translated"},
		"summary":          map[string]any{"explicit_header": "X-CodeArts-Request-Kind: summary", "automatic_detection": "Pi compaction system prompt only", "retry": "same account, at most one truncation retry; optional administrator-selected fallback model; no partial output before success"},
		"unsupported":      []string{"native responses/compact", "Responses WebSocket", "store=true", "previous_response_id", "provider-hosted tools"},
	}
}
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "仅支持 GET", "invalid_request_error")
		return
	}
	caps := gatewayCapabilities()
	caps["providers"] = s.providerDescriptors()
	caps["model_routing"] = "配置统一模型的分组按公开名称跨平台选号；其他分组保持原有路由。完整平台型号可在分组权限内直接调用。"
	writeJSON(w, 200, caps)
}
func unsupportedCapability(w http.ResponseWriter, r *http.Request) {
	writeErr(w, 501, "当前码道上游不支持此原生能力；请使用客户端文本摘要和显式历史，勿将协议转换视为原生压缩", "unsupported_capability")
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
