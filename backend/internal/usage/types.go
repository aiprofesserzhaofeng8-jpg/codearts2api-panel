// Package usage owns measured usage and historical accounting values.
package usage

// TokenUsage holds measured upstream usage. Input includes cached tokens;
// output includes reasoning tokens. Known distinguishes missing data from zero.
type TokenUsage struct {
	Known           bool  `json:"known"`
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

// LogEntry 是一条请求日志（只记元信息，不落对话内容）。
type RequestAttempt struct {
	DurationMS    *int64       `json:"duration_ms,omitempty"`
	FirstTokenMS  *int64       `json:"first_token_ms,omitempty"`
	Status        int          `json:"status,omitempty"`
	ErrorKind     string       `json:"error_kind,omitempty"`
	EstimatedCost *RequestCost `json:"estimated_cost,omitempty"`
	Number        int          `json:"number"`
	Account       string       `json:"account"`
	Model         string       `json:"model"`
	FinishReason  string       `json:"finish_reason,omitempty"`
	Usage         TokenUsage   `json:"usage"`

	// Adapter status before public error mapping; absent on success/older logs.
	UpstreamStatus int `json:"upstream_status,omitempty"`
}

type ConsumerCharge struct {
	Units  int64 `json:"units"`
	Review bool  `json:"review"`
}

type LogEntry struct {
	ConsumerCharge  *ConsumerCharge  `json:"consumer_charge,omitempty"`
	UserID          string           `json:"user_id,omitempty"`
	PublicModel     string           `json:"public_model,omitempty"`
	RequestedModel  string           `json:"requested_model,omitempty"`
	FirstTokenMS    *int64           `json:"first_token_ms,omitempty"`
	ErrorKind       string           `json:"error_kind,omitempty"`
	Provider        string           `json:"provider,omitempty"`
	Scope           string           `json:"scope,omitempty"`
	UsageIncomplete bool             `json:"usage_incomplete,omitempty"`
	AttemptDetails  []RequestAttempt `json:"attempt_details,omitempty"`
	RequestID       string           `json:"request_id,omitempty"`
	RequestKind     string           `json:"request_kind,omitempty"`
	FinishReason    string           `json:"finish_reason,omitempty"`
	Attempts        int              `json:"attempts,omitempty"`
	SummaryAttempts int              `json:"summary_attempts,omitempty"`
	EstimatedCost   *RequestCost     `json:"estimated_cost,omitempty"`
	Protocol        string           `json:"protocol,omitempty"`
	Usage           TokenUsage       `json:"usage"`
	Time            int64            `json:"time"`
	Key             string           `json:"key"`
	Account         string           `json:"account"`
	Model           string           `json:"model"`
	Stream          bool             `json:"stream"`
	Status          int              `json:"status"`
	LatencyMS       int64            `json:"latency_ms"`
	PromptLen       int              `json:"prompt_len"`
	OutputLen       int              `json:"output_len"`
	Err             string           `json:"err,omitempty"`
}

// Stats 是累计统计。
type Stats struct {
	PartialUsageRequests int64   `json:"partial_usage_requests"`
	SummaryRequests      int64   `json:"summary_requests"`
	SummaryFailures      int64   `json:"summary_failures"`
	SummaryRetries       int64   `json:"summary_retries"`
	TruncatedRequests    int64   `json:"truncated_requests"`
	EstimatedCNY         float64 `json:"estimated_cny,omitempty"`
	EstimatedUSD         float64 `json:"estimated_usd"`
	PricedRequests       int64   `json:"priced_requests"`
	PricedCNYRequests    int64   `json:"priced_cny_requests,omitempty"`
	PricedUSDRequests    int64   `json:"priced_usd_requests"`
	InputTokens          int64   `json:"input_tokens"`
	OutputTokens         int64   `json:"output_tokens"`
	CachedTokens         int64   `json:"cached_tokens"`
	ReasoningTokens      int64   `json:"reasoning_tokens"`
	UsageRequests        int64   `json:"usage_requests"`
	TotalLatencyMS       int64   `json:"total_latency_ms"`
	LatencySamples       int64   `json:"latency_samples"`
	UsageStartedAt       int64   `json:"usage_started_at"`
	TotalRequests        int64   `json:"total_requests"`
	FailedRequests       int64   `json:"failed_requests"`
	TotalCharsOut        int64   `json:"total_chars_out"`
	StartedAt            int64   `json:"started_at"`
}
