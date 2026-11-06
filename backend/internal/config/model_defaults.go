package config

// DefaultModelsApply is an explicit, versioned administrator action. Names are
// selected from a freshly reconstructed catalog, never supplied definitions.
type DefaultModelsApply struct {
	Version                 uint64   `json:"version"`
	CatalogRevision         string   `json:"catalog_revision"`
	ExpectedRoutingRevision uint64   `json:"expected_routing_revision"`
	Names                   []string `json:"names"`
	GroupID                 string   `json:"group_id"`
	ExpectedGroupRevision   uint64   `json:"expected_group_revision"`
}
