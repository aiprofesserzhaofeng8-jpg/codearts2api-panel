package config

import "encoding/json"

type SettingsUpdate struct {
	CommonPrompt      json.RawMessage   `json:"common_prompt"`
	CodeArts          json.RawMessage   `json:"codearts"`
	Trae              json.RawMessage   `json:"trae"`
	Qoder             json.RawMessage   `json:"qoder"`
	Kiro              json.RawMessage   `json:"kiro"`
	Claude            json.RawMessage   `json:"claude"`
	ChatGPT           json.RawMessage   `json:"chatgpt"`
	WorkBuddy         json.RawMessage   `json:"workbuddy"`
	ModelAliases      map[string]string `json:"model_aliases"`
	Pool              json.RawMessage   `json:"pool"`
	AutoClaim         *bool             `json:"auto_claim"`
	ClaimAfterHour    *int              `json:"claim_after_hour"`
	ClaimAfterMinute  *int              `json:"claim_after_minute"`
	DefaultModel      *string           `json:"default_model"`
	ForceModel        *string           `json:"force_model"`
	RequestTimeout    *int              `json:"request_timeout_sec"`
	MaxRetries        *int              `json:"max_retries"`
	LogKeep           *int              `json:"log_keep"`
	CORSOrigin        *string           `json:"cors_origin"`
	AdminPassword     *string           `json:"admin_password"`
	ClearPassword     *bool             `json:"clear_admin_password"`
	AutoRefresh       *bool             `json:"auto_refresh"`
	AllowCreditModels *bool             `json:"allow_credit_models"`
	AllowAnonymous    *bool             `json:"allow_anonymous"`
	RateLimitEnabled  *bool             `json:"rate_limit_enabled"`
	RateLimitQPS      *int              `json:"rate_limit_qps"`
	RateLimitBurst    *int              `json:"rate_limit_burst"`
	RateLimitConc     *int              `json:"rate_limit_concurrency"`
	WriteBackToIDE    *bool             `json:"write_back_to_ide"`
	LiveTTLSeconds    *int              `json:"live_ttl_sec"`
	UserDataDir       *string           `json:"user_data_dir"`
	LoginPort         *int              `json:"login_port"`
	PluginName        *string           `json:"plugin_name"`
	PluginVersion     *string           `json:"plugin_version"`
	IDEName           *string           `json:"ide_name"`
	IDEVersion        *string           `json:"ide_version"`
	Language          *string           `json:"language"`
}
