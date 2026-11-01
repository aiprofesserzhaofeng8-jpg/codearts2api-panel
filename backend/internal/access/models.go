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

type AccountGroup struct {
	ModelMappings    []ModelMapping `json:"model_mappings,omitempty"`
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	AccountIDs       []string       `json:"account_ids"`
	AllowlistEnabled bool           `json:"allowlist_enabled"`
	ModelIDs         []string       `json:"model_ids"`
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
