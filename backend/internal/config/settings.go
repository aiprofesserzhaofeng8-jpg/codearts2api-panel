// Package config owns configuration values, validation and management coordination.
// Persistence and atomic request admission retain their existing Store boundary.
package config

// Settings 是运行期可调参数。
type Settings struct {
	CodeArts       ProviderSettings  `json:"codearts"`
	Trae           ProviderSettings  `json:"trae"`
	WorkBuddy      WorkBuddySettings `json:"workbuddy"`
	Pool           PoolSettings      `json:"pool"`
	ModelAliases   map[string]string `json:"model_aliases"`
	DefaultModel   string            `json:"default_model"`
	ForceModel     string            `json:"force_model"`
	RequestTimeout int               `json:"request_timeout_sec"`
	MaxRetries     int               `json:"max_retries"`
	LogKeep        int               `json:"log_keep"`
	CORSOrigin     string            `json:"cors_origin"`
	AdminPassword  string            `json:"admin_password"`

	// AutoRefresh 允许在凭据过期且 IDE 未续期时自行用 refresh_token 续期。
	AutoRefresh bool `json:"auto_refresh"`
	// AllowCreditModels 允许调用真实消耗积分的模型。
	// 免费权益通道已于 2026-10 下线，现在**所有**模型都计费，
	// 因此关闭它等于拒绝一切上游调用（默认开启）。
	AllowCreditModels bool `json:"allow_credit_models"`
	// AllowAnonymous 允许在尚未签发任何 API 密钥时，从非本机来源匿名调用 /v1/*。
	//
	// 默认关闭：没密钥时只放行本机直连。置真等于把账号额度开放给能访问到
	// 这个端口的所有人，仅在你确定端口没有对外暴露时才开启。
	AllowAnonymous bool `json:"allow_anonymous"`

	// RateLimitEnabled 开启按密钥的自我保护型限流。
	//
	// 定位见 goal 的「非目标」：只防止失控客户端烧光积分，
	// **不**承诺密钥之间的公平配额。默认开启。
	RateLimitEnabled bool `json:"rate_limit_enabled"`
	// RateLimitQPS 是每个密钥长期允许的请求速率（令牌桶补充速率）。
	RateLimitQPS int `json:"rate_limit_qps"`
	// RateLimitBurst 是每个密钥允许的瞬时突发请求数（桶容量）。
	RateLimitBurst int `json:"rate_limit_burst"`
	// RateLimitConcurrency 是每个密钥允许的在飞请求上限；<= 0 表示不限。
	RateLimitConcurrency int `json:"rate_limit_concurrency"`
	// AutoClaim 每天自动领取运营活动福利（幂等，重复领不报错）。
	AutoClaim bool `json:"auto_claim"`
	// ClaimAfterHour / ClaimAfterMinute 是每天开始领取的本地时刻。
	// 运营活动的权益按自然日刷新，所以默认 00:05 领。
	ClaimAfterHour   int `json:"claim_after_hour"`
	ClaimAfterMinute int `json:"claim_after_minute"`
	// WriteBackToIDE 允许把续期结果回写 state.vscdb。
	WriteBackToIDE bool `json:"write_back_to_ide"`
	// LiveTTLSeconds 是重读 IDE 会话的缓存时长。
	LiveTTLSeconds int `json:"live_ttl_sec"`
	// UserDataDir 是 CodeArts Agent 的 User Data 目录（留空自动探测）。
	UserDataDir string `json:"user_data_dir"`
	// LoginPort 固定浏览器登录的回调端口（0 = 随机 ≥10000）。
	//
	// 远端部署时固定它，就能配一条常驻 SSH 本地转发：
	//     ssh -N -L <LoginPort>:127.0.0.1:<LoginPort> user@server
	LoginPort int `json:"login_port"`

	// 上游身份头（留空用与 26.9.902 对齐的缺省值）
	PluginName    string `json:"plugin_name"`
	PluginVersion string `json:"plugin_version"`
	IDEName       string `json:"ide_name"`
	IDEVersion    string `json:"ide_version"`
	Language      string `json:"language"`
}

// WorkBuddySettings retains the original settings field type name.
type WorkBuddySettings = ProviderSettings
