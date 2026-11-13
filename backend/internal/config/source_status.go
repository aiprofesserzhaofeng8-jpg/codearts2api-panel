package config

// SourceRuntimeStatus is an administrator-only observation, never admission
// state. Store projects its existing accounts, pool, model health and logs.
type SourceRuntimeStatus struct {
	AccountCount int                  `json:"account_count"`
	Ready        int                  `json:"ready"`
	Disabled     int                  `json:"disabled"`
	Paused       int                  `json:"paused"`
	Cooling      int                  `json:"cooling"`
	Busy         int                  `json:"busy"`
	NextRetryAt  int64                `json:"next_retry_at,omitempty"`
	LastRequest  *SourceRequestStatus `json:"last_request"`
}
type SourceRequestStatus struct {
	CheckedAt int64  `json:"checked_at"`
	Status    int    `json:"status"`
	ErrorKind string `json:"error_kind,omitempty"`
}
