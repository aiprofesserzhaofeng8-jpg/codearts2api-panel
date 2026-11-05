package catalog

import (
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
)

type ModelReference struct {
	GroupID string `json:"group_id,omitempty"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
}
type SourceStatus struct {
	ID                   string    `json:"id"`
	CatalogStatus        string    `json:"catalog_status"`
	Enabled              bool      `json:"enabled"`
	EligibleAccountCount int       `json:"eligible_account_count"`
	LastProbe            any       `json:"last_probe"`
	ObservedAt           time.Time `json:"observed_at"`
}
type UnifiedDetail struct {
	Model           config.UnifiedModel `json:"model"`
	ClientPolicy    *config.ModelPolicy `json:"client_policy"`
	PolicyKey       string              `json:"policy_key"`
	RoutingRevision uint64              `json:"routing_revision"`
	References      []ModelReference    `json:"references"`
	SourceStatus    []SourceStatus      `json:"source_status"`
}

func (m *Management) Detail(id string) (UnifiedDetail, error) {
	snapshot := m.store.UnifiedSnapshot()
	for _, model := range snapshot.Models {
		if model.ID != id {
			continue
		}
		out := UnifiedDetail{Model: model, PolicyKey: config.UnifiedPolicyKey(id), RoutingRevision: snapshot.Revision, References: []ModelReference{}, SourceStatus: []SourceStatus{}}
		if p, ok := snapshot.Policies[out.PolicyKey]; ok {
			out.ClientPolicy = &p
		}
		if m.store.Settings().DefaultModel == model.Name {
			out.References = append(out.References, ModelReference{Kind: "global_default", Name: model.Name})
		}
		if st, ok := m.store.(interface{ CanonicalGroups() []access.AccountGroup }); ok {
			for _, g := range st.CanonicalGroups() {
				for _, grant := range g.UnifiedModels {
					if grant.ModelID == id {
						out.References = append(out.References, ModelReference{g.ID, "group_grant", g.Name})
					}
				}
				if g.DefaultModel == model.Name {
					out.References = append(out.References, ModelReference{g.ID, "group_default", g.Name})
				}
				if g.ClientDefaultModel == model.Name {
					out.References = append(out.References, ModelReference{g.ID, "client_default", g.Name})
				}
			}
		}
		ids := make([]string, len(model.Sources))
		for i, src := range model.Sources {
			ids[i] = src.ID
		}
		counts, observed := map[string]int{}, time.Now().UTC()
		if st, ok := m.store.(interface {
			UnifiedSourceCounts([]string) (map[string]int, time.Time)
		}); ok {
			counts, observed = st.UnifiedSourceCounts(ids)
		}
		for _, src := range model.Sources {
			status, count := "missing", 0
			if _, ok := m.directory.ResolveRaw(src.ID); ok {
				status = "present"
				if model.Enabled && src.Enabled {
					count = counts[src.ID]
				}
			}
			out.SourceStatus = append(out.SourceStatus, SourceStatus{src.ID, status, src.Enabled, count, nil, observed})
		}
		return out, nil
	}
	return UnifiedDetail{}, config.ModelFailure(404, "model_not_found", "统一模型不存在")
}
