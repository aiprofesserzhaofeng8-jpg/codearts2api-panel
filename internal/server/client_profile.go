package server

import (
	"codearts2api/internal/apierr"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
	"encoding/json"
	"net/http"
	"strings"
)

const summaryGuidance = "Produce a concise checkpoint, not an exhaustive transcript. Keep the summary under 1800 words. Preserve required headings, user goal, active constraints, essential paths/IDs, completed work, blockers and concrete next steps. Deduplicate repeated attempts. Do not reproduce source code, long logs or full tool output. Finish every required section within this limit."
const summaryRetryGuidance = "The previous summary exceeded the output budget. This is a replacement attempt, not a continuation. Finish a much shorter checkpoint under 900 words. Preserve only actionable essentials and required headings. Do not quote the transcript."

type summaryIntentKey struct{}

func profileFromPolicy(model string, p store.ModelPolicy) map[string]any {
	return map[string]any{"version": 1, "model": model, "context_window": p.ContextWindow, "max_output_tokens": p.MaxOutputTokens,
		"context_basis": p.Source, "compaction": map[string]int{"reserve_tokens": p.ReserveTokens, "keep_recent_tokens": p.KeepRecentTokens, "trigger_tokens": p.TriggerTokens},
		"protocols": []string{"chat_completions", "responses", "anthropic_messages"}, "pi_summary_adapter": p.SummaryEnabled,
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
	p, ok := store.DefaultModelPolicies()[model]
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
	model, err := s.resolveModelForRequest(r, s.Store.Settings(), r.URL.Query().Get("model"))
	if err != nil {
		writeErr(w, apierr.From(err).Status, err.Error(), "invalid_request_error")
		return
	}
	p, ok := s.modelPolicyFor(model)
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
		writeJSON(w, 200, map[string]any{"policies": s.allModelPolicies(), "capabilities": gatewayCapabilities()})
	case http.MethodPost:
		var body struct {
			Model  string            `json:"model"`
			Policy store.ModelPolicy `json:"policy"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body) != nil {
			writeErr(w, 400, "配置格式无效", "invalid_request_error")
			return
		}
		model, ok := s.Reg.Resolve(body.Model, s.Store.Settings().ModelAliases)
		if !ok {
			writeErr(w, 400, "模型未在当前目录中", "invalid_request_error")
			return
		}
		if err := body.Policy.Validate(); err != nil {
			writeErr(w, 400, err.Error(), "invalid_request_error")
			return
		}
		fallback := strings.TrimSpace(body.Policy.SummaryFallbackModel)
		if fallback != "" {
			target, exists := s.Reg.Resolve(fallback, s.Store.Settings().ModelAliases)
			if !exists || target.ID == model.ID || modelProvider(target) != modelProvider(model) || target.Scope != model.Scope {
				writeErr(w, 400, "摘要备用模型须为相同上游和地区的其他模型", "invalid_request_error")
				return
			}
			if forced := s.Store.Settings().ForceModel; forced != "" && forced != target.ID {
				writeErr(w, 400, "请先取消强制模型，再配置其他摘要备用模型", "invalid_request_error")
				return
			}
			body.Policy.SummaryFallbackModel = target.ID
		}
		if modelProvider(model) != "codearts" && body.Policy.SummaryEnabled {
			writeErr(w, 400, "当前摘要完整性适配仅支持码道；其他上游可配置上下文建议，但请关闭服务端摘要保护", "invalid_request_error")
			return
		}
		body.Policy.Source = "管理员配置，非上游能力声明"
		if err := s.Store.SetModelPolicyAndSave(model.ID, body.Policy); err != nil {
			writeErr(w, 500, "保存模型策略失败", "server_error")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "policies": s.allModelPolicies()})
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
	caps["model_routing"] = "裸模型名保持码道；workbuddy/cn/<model> 或 workbuddy/global/<model> 显式选择 WorkBuddy"
	writeJSON(w, 200, caps)
}
func unsupportedCapability(w http.ResponseWriter, r *http.Request) {
	writeErr(w, 501, "当前码道上游不支持此原生能力；请使用客户端文本摘要和显式历史，勿将协议转换视为原生压缩", "unsupported_capability")
}

func summaryRequest(body []byte, explicit bool) bool {
	var req struct {
		Tools    []json.RawMessage `json:"tools"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Tools) > 0 {
		return false
	}
	if explicit {
		return true
	}
	for _, m := range req.Messages {
		var text string
		_ = json.Unmarshal(m.Content, &text)
		if (m.Role == "system" || m.Role == "developer") && strings.HasPrefix(text, "You are a context summarization assistant.") {
			return true
		}
	}
	return false
}
func adaptSummary(body []byte, p store.ModelPolicy, retry bool) ([]byte, error) {
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(req["messages"], &messages); err != nil {
		return nil, err
	}
	guidance := summaryGuidance
	if retry {
		guidance = summaryRetryGuidance
	}
	added := false
	for _, m := range messages {
		var role, text string
		_ = json.Unmarshal(m["role"], &role)
		_ = json.Unmarshal(m["content"], &text)
		if role == "system" || role == "developer" {
			if !strings.Contains(text, guidance) {
				text += "\n\n" + guidance
			}
			m["content"], _ = json.Marshal(text)
			added = true
			break
		}
	}
	if !added {
		role, _ := json.Marshal("system")
		text, _ := json.Marshal(guidance)
		messages = append([]map[string]json.RawMessage{{"role": role, "content": text}}, messages...)
	}
	if p.DisableSummaryThinking {
		req["thinking"] = json.RawMessage(`{"type":"disabled"}`)
		delete(req, "reasoning_effort")
	}
	cap := p.SummaryMaxTokens
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		var n int
		if json.Unmarshal(req[field], &n) == nil && n > 0 {
			cap = min(cap, n)
		}
	}
	req["max_tokens"], _ = json.Marshal(cap)
	delete(req, "max_completion_tokens")
	req["messages"], _ = json.Marshal(messages)
	req["stream"] = json.RawMessage("true")
	return requestStreamUsage(mustJSON(req))
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func adaptClientRequest(body []byte, model string) ([]byte, error) {
	p, ok := store.DefaultModelPolicies()[model]
	if !ok || !p.SummaryEnabled || !summaryRequest(body, false) {
		return body, nil
	}
	return adaptSummary(body, p, false)
}

// Recommendations for new providers use declared catalog bounds, never model-name guesses.
func (s *Server) modelPolicyFor(m registry.Model) (store.ModelPolicy, bool) {
	if p, ok := s.Store.ModelPolicy(m.ID); ok {
		return p, true
	}
	if modelProvider(m) == "codearts" || m.Context < 8192 || m.MaxOutput < 256 {
		return store.ModelPolicy{}, false
	}
	window := min(m.Context, 2097152)
	output := min(m.MaxOutput, 8192, window/4)
	reserve := min(max(16384, output+4096), window-2048)
	trigger := min(45000, window-reserve)
	keep := min(12000, trigger/2)
	p := store.ModelPolicy{ContextWindow: window, MaxOutputTokens: output, ReserveTokens: reserve, KeepRecentTokens: keep, TriggerTokens: trigger, SummaryMaxTokens: output, Source: "基于上游目录的保守建议；此上游未启用服务端摘要适配"}
	return p, p.Validate() == nil
}
func (s *Server) allModelPolicies() map[string]store.ModelPolicy {
	out := s.Store.ModelPolicies()
	for _, m := range s.Reg.List(nil) {
		if p, ok := s.modelPolicyFor(m); ok {
			out[m.ID] = p
		}
	}
	return out
}
