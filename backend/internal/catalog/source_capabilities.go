package catalog

import (
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

// Reviewed observations share the existing versioned catalog. They describe an
// exact provider service, not the identity or capabilities of its base model.
type sourceCapabilityEvidence struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ImageInput  string `json:"image_input,omitempty"`
	ToolCalling string `json:"tool_calling,omitempty"`
	Reference   string `json:"reference"`
	VerifiedAt  string `json:"verified_at"`
	Basis       string `json:"basis"`
}

func reviewedSourceCapabilities(model provider.Model) provider.Model {
	// Listing/profile placeholders for a missing source have no catalog metadata.
	// Reviewed observations must not turn them into available/verified sources.
	if model.Name == "" && model.Source == "" && model.UpstreamID == "" {
		return model
	}
	id := config.CanonicalSource(model.ID)
	if model.UpstreamID != "" {
		id = config.CanonicalSource(provider.ModelID(ModelProvider(model), model.Scope, model.UpstreamID))
	}
	for _, evidence := range defaultCatalog.SourceCapabilities {
		if evidence.ID != id || (evidence.Name != "" && evidence.Name != model.Name) {
			continue
		}
		if model.ImageInput == "" {
			model.ImageInput = evidence.ImageInput
		}
		if model.ToolCalling == "" {
			model.ToolCalling = evidence.ToolCalling
		}
		break
	}
	return model
}
