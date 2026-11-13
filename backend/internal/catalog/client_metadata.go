package catalog

import (
	"strings"

	"codearts2api/internal/catalog/modelmeta"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// Optional reviewed recommendations share the existing catalog. They never
// change provider observations or claim that a tested operating point is a max.
type sourceClientMetadata struct {
	IDs             []string `json:"ids"`
	Purpose         string   `json:"purpose"`
	ContextWindow   int      `json:"context_window,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
	Basis           string   `json:"basis"`
	Reference       string   `json:"reference"`
	VerifiedAt      string   `json:"verified_at"`
}

type ClientLimitEvidence struct {
	OfficialContext   string `json:"official_context,omitempty"`
	OfficialMaxOutput string `json:"official_max_output,omitempty"`
	OfficialReference string `json:"official_reference,omitempty"`
	Source            string `json:"source"`
	Basis             string `json:"basis"`
	ContextWindow     int    `json:"context_window,omitempty"`
	MaxOutputTokens   int    `json:"max_output_tokens,omitempty"`
	ProviderContext   int    `json:"provider_context,omitempty"`
	ProviderMaxOutput int    `json:"provider_max_output,omitempty"`
	Reference         string `json:"reference,omitempty"`
	VerifiedAt        string `json:"verified_at,omitempty"`
}

// ClientCompatibility is an export recommendation, not an admission or health
// decision. Unsupported/unknown entries remain visible and explicitly callable.
type ClientCompatibility struct {
	Purpose    string                `json:"purpose"`
	Exportable bool                  `json:"exportable"`
	Status     string                `json:"status"`
	Reason     string                `json:"reason"`
	Limits     []ClientLimitEvidence `json:"limits"`
}

func clientMetadata(model provider.Model) (sourceClientMetadata, bool) {
	if model.Name == "" && model.Source == "" && model.UpstreamID == "" && model.Context == 0 {
		return sourceClientMetadata{}, false // Missing directory placeholder.
	}
	id := config.CanonicalSource(model.ID)
	if model.UpstreamID != "" {
		id = config.CanonicalSource(provider.ModelID(ModelProvider(model), model.Scope, model.UpstreamID))
	}
	if alias, ok := qoderCNAlias(id); ok && !strings.EqualFold(strings.TrimSpace(model.Name), alias.DisplayName) {
		return sourceClientMetadata{}, false
	}
	for _, entry := range defaultCatalog.ClientMetadata {
		for _, source := range entry.IDs {
			if id == source {
				return entry, true
			}
		}
	}
	return sourceClientMetadata{}, false
}

func sourcePurpose(model provider.Model) string {
	// Purpose describes the provider operation, independently of whether its
	// current directory entry or numeric limits are present.
	id := config.CanonicalSource(model.ID)
	if model.UpstreamID != "" {
		id = config.CanonicalSource(provider.ModelID(ModelProvider(model), model.Scope, model.UpstreamID))
	}
	for _, entry := range defaultCatalog.ClientMetadata {
		for _, source := range entry.IDs {
			if id == source && entry.Purpose != "" {
				return entry.Purpose
			}
		}
	}
	p, _, upstream := provider.ParseModel(model.ID)
	if upstream == "auto" && (p == provider.WorkBuddy || p == provider.Qoder || p == provider.Kiro) {
		return "auto"
	}
	return "chat" // A model advertised by an existing chat adapter, not a tool/image guarantee.
}

func RecommendClientPolicy(model provider.Model) (config.ModelPolicy, bool) {
	p, ok, _, _, _ := recommendClientPolicy(model)
	return p, ok
}

// Follow Sub2API's missing-only metadata merge and client-template fallback.
// Our platforms use a 32K/4K operating default rather than Codex's 272K default.
// No default is a claim about the provider maximum, image or tool support.
func recommendClientPolicy(model provider.Model) (config.ModelPolicy, bool, string, string, string) {
	if model.Name == "" && model.Source == "" && model.UpstreamID == "" && model.Context == 0 {
		return config.ModelPolicy{}, false, "unknown", "", ""
	}
	fallback := modelmeta.UpstreamModelMetadata{ContextWindow: 32768, MaxOutputTokens: 4096}
	basis, reference, verified := "compatibility_default", "docs/CLIENTS.md", ""
	if ref := model.ClientReference; ref != nil && (ref.ContextWindow > 0 || ref.MaxOutputTokens > 0) {
		fallback = modelmeta.Merge(modelmeta.UpstreamModelMetadata{ContextWindow: int64(ref.ContextWindow), MaxOutputTokens: int64(ref.MaxOutputTokens)}, fallback)
		basis, reference = "public_registry", ref.Reference
	}
	if entry, ok := clientMetadata(model); ok && entry.ContextWindow > 0 && entry.MaxOutputTokens > 0 {
		fallback = modelmeta.Merge(modelmeta.UpstreamModelMetadata{ContextWindow: int64(entry.ContextWindow), MaxOutputTokens: int64(entry.MaxOutputTokens)}, fallback)
		basis, reference, verified = entry.Basis, entry.Reference, entry.VerifiedAt
	}
	if model.Context > 0 && model.MaxOutput > 0 {
		basis, reference, verified = "provider_catalog", "", ""
	}
	if purpose := sourcePurpose(model); basis == "compatibility_default" && purpose != "chat" && purpose != "auto" {
		return config.ModelPolicy{}, false, "unknown", "", ""
	}
	merged := modelmeta.Merge(modelmeta.UpstreamModelMetadata{ContextWindow: int64(model.Context), MaxOutputTokens: int64(model.MaxOutput)}, fallback)
	p, ok := config.RecommendModelPolicy(provider.Model{Provider: "client_template", Context: int(min(merged.ContextWindow, 2097152)), MaxOutput: int(min(merged.MaxOutputTokens, 2097152))})
	switch basis {
	case "provider_catalog":
		p.Source = "基于上游目录的保守客户端建议"
	case "tested_recommendation":
		p.Source = "精确来源的已验证客户端建议，非上游最大值；" + reference + " · " + verified
	case "public_registry":
		p.Source = "缺失参数参考 models.dev 公开资料，非平台实测上限"
	default:
		p.Source = "兼容默认客户端参数，非平台实测上限；可在模型页调整"
	}
	return p, ok, basis, reference, verified
}

func sourceClientPolicy(model provider.Model, configured map[string]config.ModelPolicy) (config.ModelPolicy, bool, ClientLimitEvidence) {
	id := config.RawSource(model.ID)
	evidence := ClientLimitEvidence{Source: config.CanonicalSource(id), Basis: "unknown", ProviderContext: model.Context, ProviderMaxOutput: model.MaxOutput}
	if native := NativeModelContract(model); native != nil && (native.ContextLabel != "" || native.MaxOutputLabel != "") {
		evidence.OfficialContext, evidence.OfficialMaxOutput, evidence.OfficialReference = native.ContextLabel, native.MaxOutputLabel, native.Reference
	}
	p, ok := configured[id]
	if ok {
		evidence.Basis = "administrator_config"
	} else if p, ok = config.DefaultModelPolicies()[id]; ok {
		evidence.Basis = "tested_recommendation"
		evidence.Reference = "docs/CLIENTS.md"
	} else {
		p, ok, evidence.Basis, evidence.Reference, evidence.VerifiedAt = recommendClientPolicy(model)
	}
	if ok {
		evidence.ContextWindow, evidence.MaxOutputTokens = p.ContextWindow, p.MaxOutputTokens
	}
	return p, ok, evidence
}

func clientCompatibility(models []provider.Model, policy *config.ModelPolicy, configured map[string]config.ModelPolicy) ClientCompatibility {
	out := ClientCompatibility{Purpose: "unknown", Status: "missing_limits", Limits: []ClientLimitEvidence{}}
	missing := []string{}
	for i, model := range models {
		purpose := sourcePurpose(model)
		if i == 0 {
			out.Purpose = purpose
		} else if out.Purpose != purpose {
			if (out.Purpose == "chat" || out.Purpose == "auto") && (purpose == "chat" || purpose == "auto") {
				out.Purpose = "auto"
			} else {
				out.Purpose = "mixed"
			}
		}
		_, ok, evidence := sourceClientPolicy(model, configured)
		out.Limits = append(out.Limits, evidence)
		if !ok {
			missing = append(missing, evidence.Source)
		}
	}
	if out.Purpose != "chat" && out.Purpose != "auto" {
		out.Status, out.Reason = "unsupported_purpose", "此入口用途为 "+out.Purpose+"，不自动导出到通用聊天客户端"
		return out
	}
	if policy == nil || policy.Validate() != nil {
		out.Reason = "缺少可验证的客户端参数"
		if len(missing) > 0 {
			out.Reason += "：" + strings.Join(missing, ", ")
		}
		return out
	}
	out.Status, out.Exportable = "ready", true
	if out.Purpose == "auto" {
		out.Reason = "平台自动路由入口，实际底模可能变化"
	}
	for _, limit := range out.Limits {
		if limit.Basis == "compatibility_default" || limit.Basis == "public_registry" {
			if out.Reason != "" {
				out.Reason += "；"
			}
			out.Reason += "部分客户端参数采用公开资料或兼容默认值，非平台实测上限，可在模型页调整"
			break
		}
	}
	return out
}

func SourceClientCompatibility(model provider.Model, policy *config.ModelPolicy, configured map[string]config.ModelPolicy) ClientCompatibility {
	return clientCompatibility([]provider.Model{model}, policy, configured)
}

func publicClientCompatibility(model provider.Model, policy *config.ModelPolicy, out ClientCompatibility) ClientCompatibility {
	if model.Provider != "unified" && model.Provider != "group" {
		return out
	}
	// Precise source identities and evidence belong to the administrator detail.
	// Public shared names expose only the compatible aggregate recommendation.
	evidence := ClientLimitEvidence{Source: "authorized_sources", Basis: "common_authorized_sources"}
	if policy != nil {
		evidence.ContextWindow, evidence.MaxOutputTokens = policy.ContextWindow, policy.MaxOutputTokens
	} else {
		evidence.Basis = "unknown"
		if out.Status == "missing_limits" {
			out.Reason = "部分授权来源缺少可验证的客户端参数，请联系管理员查看模型详情"
		}
	}
	out.Limits = []ClientLimitEvidence{evidence}
	return out
}

// Profile already resolved the authorized candidates. This projection does not
// select accounts, broaden candidates or query an upstream.
func (s *Routing) ClientCompatibility(model provider.Model, plan *ModelPlan, policy *config.ModelPolicy) ClientCompatibility {
	configured := map[string]config.ModelPolicy{}
	if store, ok := s.store.(UnifiedRoutingStore); ok {
		configured = store.UnifiedSnapshot().Policies
	}
	models := []provider.Model{model}
	if plan != nil {
		models = nil
		for _, id := range plan.IDs {
			models = append(models, plan.Models[id])
		}
	}
	return publicClientCompatibility(model, policy, clientCompatibility(models, policy, configured))
}
