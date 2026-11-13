// SPDX-License-Identifier: LGPL-3.0-only
package modelmeta

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// Registry is public reference data, never a list of available gateway models.
// Its immutable value is confined to the existing catalog refresh runtime.
type Registry struct{ providers map[string]modelsDevProvider }

const RegistryURL = "https://models.dev/api.json"
const BodyLimit = 8 << 20

func Parse(body []byte) (*Registry, error) {
	if len(body) > BodyLimit {
		return nil, errors.New("model reference registry exceeds size limit")
	}
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(body, &providers); err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, errors.New("empty model reference registry")
	}
	for _, p := range providers {
		if len(p.Models) > 0 {
			return &Registry{providers: providers}, nil
		}
	}
	return nil, errors.New("model reference registry contains no models")
}

// Lookup uses a complete, already resolved native ID. No prefixes, fuzzy
// matches or aliases are inferred from the registry. Provider order is explicit.
func (r *Registry) Lookup(id string, providers []string) (UpstreamModelMetadata, bool) {
	if r == nil || strings.TrimSpace(id) == "" {
		return UpstreamModelMetadata{}, false
	}
	for _, name := range providers {
		p := r.providers[name]
		if m, ok := p.Models[id]; ok {
			return upstreamMetadataFromModelsDevModel(id, m), true
		}
		// Same exact/case-insensitive matching as Sub2API, with stable ordering.
		keys := make([]string, 0, len(p.Models))
		for key := range p.Models {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			m := p.Models[key]
			if strings.EqualFold(key, id) || strings.EqualFold(m.ID, id) {
				return upstreamMetadataFromModelsDevModel(id, m), true
			}
		}
	}
	return UpstreamModelMetadata{}, false
}

func Merge(primary, fallback UpstreamModelMetadata) UpstreamModelMetadata {
	merged, _ := mergeUpstreamModelMetadata(primary, fallback)
	return merged
}
