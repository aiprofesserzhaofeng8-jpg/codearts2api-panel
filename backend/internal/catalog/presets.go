package catalog

import (
	"codearts2api/internal/access"
	_ "embed"
	"encoding/json"
)

//go:embed mapping_presets.json
var presetJSON []byte

// Immutable process-owned metadata. Never persisted or expanded on request
// admission; saving a preset creates an ordinary explicit group binding.
var presets = func() struct {
	Version  int                   `json:"version"`
	Mappings []access.ModelMapping `json:"mappings"`
} {
	var p struct {
		Version  int                   `json:"version"`
		Mappings []access.ModelMapping `json:"mappings"`
	}
	if err := json.Unmarshal(presetJSON, &p); err != nil {
		panic(err)
	}
	return p
}()

type PresetSource struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
}
type MappingPreset struct {
	Name    string         `json:"name"`
	Sources []PresetSource `json:"sources"`
}
type MappingPresets struct {
	Version  int             `json:"version"`
	Mappings []MappingPreset `json:"mappings"`
}

func (s *Routing) Presets() MappingPresets {
	out := MappingPresets{Version: presets.Version, Mappings: []MappingPreset{}}
	for _, m := range presets.Mappings {
		entry := MappingPreset{Name: m.Name, Sources: []PresetSource{}}
		for _, id := range m.Sources {
			resolved, ok := s.directory.ResolveRaw(id)
			entry.Sources = append(entry.Sources, PresetSource{ID: id, Available: ok && resolved.ID == id})
		}
		out.Mappings = append(out.Mappings, entry)
	}
	return out
}
