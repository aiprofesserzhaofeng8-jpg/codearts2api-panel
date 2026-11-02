// Package access owns group and API key configuration. Persistence and atomic
// request admission remain at their existing store boundary.
package access

type APIKey struct {
	GroupID     string      `json:"group_id,omitempty"`
	Budget      KeyBudget   `json:"budget"`
	BudgetUsage BudgetUsage `json:"budget_usage"`
	Key         string      `json:"key"`
	Name        string      `json:"name"`
	Enabled     bool        `json:"enabled"`
	CreatedAt   int64       `json:"created_at"`
	LastUsed    int64       `json:"last_used"`
	Requests    int64       `json:"requests"`
}

// ModelRule overrides listing and admission independently. Nil inherits the
// existing default; a disabled physical source is excluded from every mapping.
type ModelRule struct {
	ID       string `json:"id"`
	Visible  *bool  `json:"visible,omitempty"`
	Callable *bool  `json:"callable,omitempty"`
}

type AccountGroup struct {
	DefaultModel       string         `json:"default_model,omitempty"`
	ClientDefaultModel string         `json:"client_default_model,omitempty"`
	ModelRules         []ModelRule    `json:"model_rules,omitempty"`
	ModelMappings      []ModelMapping `json:"model_mappings,omitempty"`
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	AccountIDs         []string       `json:"account_ids"`
	AllowlistEnabled   bool           `json:"allowlist_enabled"`
	ModelIDs           []string       `json:"model_ids"`
}

type ModelMapping struct {
	Name    string   `json:"name"`
	Sources []string `json:"sources"`
}

type KeyBudget struct {
	MaxRequests int64   `json:"max_requests"`
	MaxTokens   int64   `json:"max_tokens"`
	MaxUSD      float64 `json:"max_usd"`
}

type BudgetUsage struct {
	Unpriced   bool    `json:"unpriced"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	USD        float64 `json:"usd"`
	Incomplete bool    `json:"incomplete"`
}

// AccessScope is a read-only permission snapshot, not an admission transaction.
type AccessScope struct {
	DefaultModel       string
	ClientDefaultModel string
	ModelRules         []ModelRule
	ModelMappings      []ModelMapping
	Restricted         bool
	AccountIDs         map[string]bool
	AllowlistEnabled   bool
	ModelIDs           map[string]bool
}

func (p AccessScope) AllowsModel(id string) bool {
	return (!p.Restricted || !p.AllowlistEnabled || p.ModelIDs[id]) && p.Callable(id)
}

func (p AccessScope) Mapping(name string) ([]string, bool) {
	for _, m := range p.ModelMappings {
		if m.Name == name {
			return m.Sources, true
		}
	}
	return nil, false
}

func (p AccessScope) Callable(id string) bool {
	for _, r := range p.ModelRules {
		if r.ID == id && r.Callable != nil {
			return *r.Callable
		}
	}
	return true
}
func (p AccessScope) Visible(id string, fallback bool) bool {
	for _, r := range p.ModelRules {
		if r.ID == id && r.Visible != nil {
			return *r.Visible
		}
	}
	return fallback
}
func CloneModelRules(in []ModelRule) []ModelRule {
	if in == nil {
		return nil
	}
	out := append([]ModelRule{}, in...)
	// One backing allocation for all optional bools, rather than one per flag.
	if len(out) == 0 {
		return out
	}
	values := make([]bool, len(out)*2)
	for i := range out {
		if out[i].Visible != nil {
			values[2*i] = *out[i].Visible
			out[i].Visible = &values[2*i]
		}
		if out[i].Callable != nil {
			values[2*i+1] = *out[i].Callable
			out[i].Callable = &values[2*i+1]
		}
	}
	return out
}
