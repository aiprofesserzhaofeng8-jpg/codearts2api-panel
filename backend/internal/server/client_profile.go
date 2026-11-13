package server

import (
	"codearts2api/internal/apierr"
	"codearts2api/internal/catalog"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
	"codearts2api/internal/storage"
	"encoding/json"
	"errors"
	"net/http"
)

type summaryIntentKey struct{}

func addImageCapability(out map[string]any, model provider.Model) {
	basis := "provider_catalog"
	contract := model.NativeModel
	if model.Provider == "unified" || model.Provider == "group" {
		basis = "authorized_sources"
		if contract != nil {
			basis = "native_and_authorized_sources"
		}
	} else {
		contract = catalog.NativeModelContract(model)
	}
	out["capability_basis"] = basis
	if contract != nil {
		out["native_model"] = contract
	}
	if input := model.InputModalities(); input != nil {
		out["input_modalities"] = input
		out["supports_images"] = model.ImageInput == "supported"
	}
	if model.ToolCalling == "supported" || model.ToolCalling == "unsupported" {
		out["supports_tools"] = model.ToolCalling == "supported"
	}
}

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
	w.Header().Set("Cache-Control", "no-store")
	model, plan, p, err := s.Routing.Profile(groupKey(r.Context()), r.URL.Query().Get("model"), s.ModelPolicies)
	if err != nil {
		var problem *config.ModelError
		if errors.As(err, &problem) && problem.Reason == "client_profile_missing" && model.ID != "" {
			profile := map[string]any{"version": 1, "model": model.ID, "client_profile_status": "missing_limits", "client_profile_error": map[string]string{"code": problem.Reason, "message": problem.Message}, "protocols": []string{"chat_completions", "responses", "anthropic_messages"}}
			addImageCapability(profile, model)
			addUnifiedProfile(profile, plan)
			addClientCompatibility(profile, s.Routing.ClientCompatibility(model, plan, nil))
			writeJSON(w, 200, profile)
			return
		}
		writeErr(w, apierr.From(err).Status, err.Error(), apierr.From(err).OpenAI, apierr.Reason(err))
		return
	}
	compatibility := s.Routing.ClientCompatibility(model, plan, &p)
	profile := profileFromPolicy(model.ID, p)
	if !compatibility.Exportable {
		profile = map[string]any{"version": 1, "model": model.ID, "protocols": []string{"chat_completions", "responses", "anthropic_messages"}}
	}
	addImageCapability(profile, model)
	addUnifiedProfile(profile, plan)
	addClientCompatibility(profile, compatibility)
	writeJSON(w, 200, profile)
}

func addClientCompatibility(out map[string]any, compatibility catalog.ClientCompatibility) {
	out["client_compatibility"] = compatibility
	out["client_profile_status"] = compatibility.Status
	if !compatibility.Exportable {
		code := "client_profile_missing"
		if compatibility.Status == "unsupported_purpose" {
			code = "client_purpose_unsupported"
		}
		out["client_profile_error"] = map[string]string{"code": code, "message": compatibility.Reason}
	}
}

func addUnifiedProfile(profile map[string]any, plan *groupModelPlan) {
	if plan == nil || plan.Selection.ID == "" {
		return
	}
	profile["model_id"] = plan.Selection.ID
	profile["model_revision"] = plan.Selection.Revision
	profile["routing_revision"] = plan.Selection.RoutingRevision
	profile["upstream_protocol"] = "routed"
}
func (s *Server) handleAdminModelPolicies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		policies := s.ModelPolicies.All()
		compatibility := map[string]catalog.ClientCompatibility{}
		snapshot := s.Store.UnifiedSnapshot()
		for _, model := range s.Reg.List(nil) {
			p, ok := policies[model.ID]
			var policy *config.ModelPolicy
			if ok {
				policy = &p
			}
			compatibility[model.ID] = catalog.SourceClientCompatibility(model, policy, snapshot.Policies)
		}
		writeJSON(w, 200, map[string]any{"policies": policies, "client_compatibility": compatibility, "capabilities": gatewayCapabilities(), "routing_revision": snapshot.Revision})
	case http.MethodPost:
		var body struct {
			Model                   string             `json:"model"`
			Policy                  config.ModelPolicy `json:"policy"`
			ExpectedRoutingRevision uint64             `json:"expected_routing_revision"`
		}
		if _, ok := decodeModelObject(w, r, 8192, &body, "model", "policy"); !ok {
			return
		}
		if err := s.Configuration.SaveModelPolicyChecked(body.Model, body.Policy, body.ExpectedRoutingRevision, s.ModelPolicies.All()); err != nil {
			writeModelError(w, err, 400)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "policies": s.ModelPolicies.All(), "routing_revision": s.Store.UnifiedSnapshot().Revision})
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
	caps["model_routing"] = "全局统一模型按公开名称跨平台选号；分组进一步限制账号、模型及来源。不分组密钥可识别全局定义。完整平台型号仍按独立权限调用。"
	writeJSON(w, 200, caps)
}
func unsupportedCapability(w http.ResponseWriter, r *http.Request) {
	writeErr(w, 501, "当前码道上游不支持此原生能力；请使用客户端文本摘要和显式历史，勿将协议转换视为原生压缩", "unsupported_capability")
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
