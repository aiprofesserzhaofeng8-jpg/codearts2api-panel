package catalog

import (
	"strings"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func TestReviewedSourcesCloseExactHistoricalGaps(t *testing.T) {
	seen := map[string]bool{}
	for _, evidence := range defaultCatalog.SourceCapabilities {
		if seen[evidence.ID] || evidence.VerifiedAt == "" || evidence.Reference == "" || evidence.Basis != "exact_source_live_roundtrip" {
			t.Fatal("invalid source evidence", evidence)
		}
		seen[evidence.ID] = true
		for _, capability := range []string{"image", "tool"} {
			if (capability == "image" && evidence.ImageInput != "supported") || (capability == "tool" && evidence.ToolCalling != "supported") {
				continue
			}
			t.Run(evidence.ID+"/"+capability, func(t *testing.T) {
				model := provider.Model{ID: evidence.ID, Name: evidence.Name, Source: "upstream"}
				plan := &ModelPlan{Public: "reviewed-service", Selection: access.UnifiedSelection{ID: "fixture"}, IDs: []string{model.ID}, Models: map[string]provider.Model{model.ID: model}}
				body := []byte(`{"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function"}]}`)
				if capability == "image" {
					body = []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}]}`)
				}
				if err := plan.FilterCapabilities(body); err != nil {
					t.Fatal("reviewed exact source still rejected", err)
				}
				m := plan.PublicModel()
				if (capability == "image" && m.ImageInput != "supported") || (capability == "tool" && m.ToolCalling != "supported") {
					t.Fatal("projection disagrees with admission", m)
				}
				if model.ImageInput != "" || model.ToolCalling != "" || m.Context != 0 || m.MaxOutput != 0 {
					t.Fatal("mutated observations or invented client limits")
				}
			})
		}
	}
}

func TestReviewedSourceFallbackPreservesUnknownNegativeAndIdentity(t *testing.T) {
	for _, model := range []provider.Model{
		{ID: "trae/cn/glm-5", Name: "glm-5", ToolCalling: "unsupported"},
		{ID: "trae/global/glm-5", Name: "glm-5"},
		{ID: "trae/cn/glm-5-unknown", Name: "glm-5"},
		{ID: "trae/cn/glm-5"}, // missing-catalog placeholder
		{ID: "qoder/cn/qfmodel", Name: "Changed model identity"},
	} {
		got := reviewedSourceCapabilities(model)
		if got.ToolCalling != model.ToolCalling || got.ImageInput != model.ImageInput {
			t.Fatal("overrode negative, unknown or changed identity", model, got)
		}
	}
	// An upstream negative wins even where the native contract supports tools.
	negative := provider.Model{ID: "trae/cn/glm-5", Name: "GLM-5", ToolCalling: "unsupported", Source: "upstream"}
	p := &ModelPlan{Public: "glm-5", Selection: access.UnifiedSelection{ID: "fixture"}, IDs: []string{negative.ID}, Models: map[string]provider.Model{negative.ID: negative}}
	if err := p.FilterCapabilities([]byte(`{"tools":[{"type":"function"}]}`)); err == nil {
		t.Fatal("explicit upstream negative bypassed")
	}
	// Exact live provider evidence cannot lift the native upper bound.
	p.Public = "glm-5.3"
	m := provider.Model{ID: "qoder/cn/auto", Name: "Auto", Source: "upstream"}
	p.IDs = []string{m.ID}
	p.Models = map[string]provider.Model{m.ID: m}
	if err := p.FilterCapabilities([]byte(`{"messages":[{"content":[{"type":"image_url"}]}]}`)); err == nil || !strings.Contains(err.Error(), "原生") {
		t.Fatal("provider image evidence overrode native contract", err)
	}
	alias := reviewedSourceCapabilities(provider.Model{ID: "user-alias", Provider: provider.Trae, Scope: "cn", UpstreamID: "glm-5", Name: "GLM-5"})
	if alias.ToolCalling != "supported" || alias.ID != "user-alias" {
		t.Fatal("alias lost source evidence or identity", alias)
	}
	if config.CanonicalSource("glm-5") != "codearts/glm-5" {
		t.Fatal("source namespace changed")
	}
}
