package access

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type UnifiedGrant struct {
	ModelID    string   `json:"model_id"`
	Visible    bool     `json:"visible"`
	Callable   bool     `json:"callable"`
	SourceMode string   `json:"source_mode"`
	SourceIDs  []string `json:"source_ids"`
}

// UnifiedSelection travels with one request. It is never an authorization token.
type UnifiedSelection struct {
	ID              string
	Revision        uint64
	RoutingRevision uint64
}

func (s AccessScope) UnifiedGrant(id string) (UnifiedGrant, bool) {
	if !s.Restricted {
		return UnifiedGrant{ModelID: id, Visible: true, Callable: true, SourceMode: "all", SourceIDs: []string{}}, true
	}
	for _, g := range s.UnifiedModels {
		if g.ModelID == id {
			return g, true
		}
	}
	return UnifiedGrant{}, false
}
func (g UnifiedGrant) AllowsSource(id string) bool {
	if g.SourceMode == "all" {
		return true
	}
	if g.SourceMode != "selected" {
		return false
	}
	for _, s := range g.SourceIDs {
		if s == id {
			return true
		}
	}
	return false
}

func (g *UnifiedGrant) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, k := range []string{"model_id", "visible", "callable", "source_mode", "source_ids"} {
		if v, ok := fields[k]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("统一模型授权字段缺失: %s", k)
		}
	}
	type plain UnifiedGrant
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode((*plain)(g))
}
func CloneUnifiedGrants(in []UnifiedGrant) []UnifiedGrant {
	if in == nil {
		return nil
	}
	out := append([]UnifiedGrant{}, in...)
	for i := range out {
		out[i].SourceIDs = append([]string{}, out[i].SourceIDs...)
	}
	return out
}
