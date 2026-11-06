package catalog

import (
	"errors"
	"strings"
	"testing"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

func nativePlan(public, raw, image string) *ModelPlan {
	return &ModelPlan{Public: public, Selection: access.UnifiedSelection{ID: "fixture"}, IDs: []string{raw}, Models: map[string]provider.Model{raw: {ID: raw, ImageInput: image, ToolCalling: "supported"}}}
}

func TestNativeCatalogEvidenceAndExactVariants(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range defaultCatalog.NativeModels {
		if seen[m.ID] || m.ID != strings.ToLower(m.ID) || !strings.HasPrefix(m.Reference, "https://") || m.VerifiedAt == "" || (m.ImageInput != "supported" && m.ImageInput != "unsupported") {
			t.Fatal("invalid native evidence", m)
		}
		seen[m.ID] = true
	}
	for _, id := range []string{"glm-5.3-unknown", "glm-5.2-sft-harmony", "deepseek-v4-pro-official", "deepseek-v4.1-flash-unknown", "trae/cn/custom_model_gemini"} {
		if c := NativeModelContract(provider.Model{ID: id}); c != nil {
			t.Fatal("guessed a variant's identity", id, c)
		}
	}
	if c := NativeModelContract(provider.Model{ID: "GLM-5.3"}); c == nil || c.ImageInput != "unsupported" {
		t.Fatal("case-only spelling lost contract", c)
	}
	qoder := provider.Model{ID: "qoder/cn/gmodel", Name: "GLM-5.3"}
	if c := NativeModelContract(qoder); c == nil || c.ID != "glm-5.3" {
		t.Fatal("verified alias lost native contract", c)
	}
	qoder.Name = "An updated unknown model"
	if c := NativeModelContract(qoder); c != nil {
		t.Fatal("stale provider alias assigned a native identity", c)
	}
}

func TestNativeTextModelRejectsProviderVisionWithoutChangingRawCatalog(t *testing.T) {
	image := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}]}`)
	for _, model := range []string{"glm-5", "glm-5-turbo", "glm-5.2", "glm-5.3", "deepseek-v4-pro", "deepseek-v4-flash", "hy3"} {
		t.Run(model, func(t *testing.T) {
			for _, public := range []string{model, "my-native-alias"} {
				p := nativePlan(public, "workbuddy/cn/"+model, "supported")
				projected := p.PublicModel()
				if projected.ImageInput != "unsupported" || projected.ToolCalling != "supported" || projected.NativeModel == nil {
					t.Fatal("platform enhancement leaked into native name", projected)
				}
				for _, unified := range []bool{true, false} {
					if !unified {
						p.Selection.ID = ""
					}
					err := p.FilterCapabilities(image)
					var failure *config.ModelError
					if !errors.As(err, &failure) || failure.Status != 400 || failure.Reason != "native_model_capability_unsupported" || !failure.NonRetryableRequest() {
						t.Fatal("native image request reached routing", err)
					}
				}
				if p.Models[p.IDs[0]].ImageInput != "supported" || len(p.IDs) != 1 {
					t.Fatal("mutated provider observations or candidates", p)
				}
				if err := p.FilterCapabilities([]byte(`{"messages":[{"role":"user","content":"text"}],"tools":[{"type":"function"}]}`)); err != nil {
					t.Fatal("text/tool capability removed", err)
				}
			}
		})
	}
}

func TestNativeVisionRequiresSourceSupport(t *testing.T) {
	for _, name := range []string{"glm-5.3-flash", "glm-5.3-flashx", "deepseek-v4.1-flash", "deepseek-v4.1-flash-1017"} {
		for _, capability := range []string{"supported", "unsupported", ""} {
			p := nativePlan(name, "workbuddy/cn/"+name, capability)
			m := p.PublicModel()
			if m.ImageInput != capability || m.NativeModel == nil || m.NativeModel.ImageInput != "supported" {
				t.Fatal("native spec manufactured source support", m)
			}
		}
	}
	// A native name's contract still wins when an administrator binds an unknown
	// provider route. It must not gain vision merely from that route's metadata.
	p := nativePlan("glm-5.3", "workbuddy/cn/auto", "supported")
	if p.PublicModel().ImageInput != "unsupported" {
		t.Fatal("unknown source raised native upper bound")
	}
	unknown := nativePlan("provider-service-1", "workbuddy/cn/private-service", "supported").PublicModel()
	if unknown.NativeModel != nil || unknown.ImageInput != "supported" {
		t.Fatal("invented native identity for a provider service", unknown)
	}
}
