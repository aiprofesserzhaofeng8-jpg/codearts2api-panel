package catalog

import (
	"strings"

	"codearts2api/internal/provider"
)

// NativeModelContract returns a reviewed exact model specification, never a
// family-prefix guess. This shares the existing versioned catalog; no network
// calls, permissions, account selection or runtime observations live here.
func NativeModelContract(model provider.Model) *provider.NativeModelContract {
	name := model.ID
	if strings.Contains(name, "/") {
		// Provider aliases require the same display-name check as default naming.
		name, _ = defaultModelName(model)
		if _, ok := qoderCNAlias(model.ID); !ok {
			_, _, upstream := provider.ParseModel(model.ID)
			if contract := nativeModelByName(upstream); contract != nil {
				return contract
			}
		}
	}
	return nativeModelByName(name)
}

func nativeModelByName(name string) *provider.NativeModelContract {
	for _, contract := range defaultCatalog.NativeModels {
		if strings.EqualFold(contract.ID, name) {
			return &contract
		}
	}
	return nil
}

// Native support is an upper bound, not proof of an adapter's ability. Unknown
// source capabilities remain unknown even when the native model supports them.
func constrainNative(model provider.Model, contract *provider.NativeModelContract) provider.Model {
	if contract != nil {
		if contract.ImageInput == "unsupported" {
			model.ImageInput = "unsupported"
		}
		if contract.ToolCalling == "unsupported" {
			model.ToolCalling = "unsupported"
		}
	}
	return model
}

func (p *ModelPlan) sourceCapabilities(model provider.Model) provider.Model {
	model = reviewedSourceCapabilities(model)
	model = constrainNative(model, NativeModelContract(model))
	return constrainNative(model, nativeModelByName(p.Public))
}

// A custom public alias inherits a native specification only when every source
// resolves to that same reviewed native ID. Mixed/unknown routes stay explicit.
func (p *ModelPlan) nativeContract() *provider.NativeModelContract {
	if contract := nativeModelByName(p.Public); contract != nil {
		return contract
	}
	var result *provider.NativeModelContract
	for _, id := range p.IDs {
		contract := NativeModelContract(p.Models[id])
		if contract == nil || (result != nil && result.ID != contract.ID) {
			return nil
		}
		result = contract
	}
	return result
}
