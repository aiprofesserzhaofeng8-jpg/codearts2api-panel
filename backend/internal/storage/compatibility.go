package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/config"
)

// legacyDefaultModels 是已不存在的旧默认模型名。
//
// 免费权益通道下线后这些名字在上游一律报 4005，启动时把它们迁移到当前默认模型。
var legacyDefaultModels = map[string]bool{
	"glm-5.3-flash":          true,
	"deepseek-v4-flash-0731": true,
	"deepseek-v4-pro-0813":   true,
}

// DefaultSettings 返回默认设置。
func DefaultSettings() Settings {
	return Settings{
		CommonPrompt:   config.DefaultPromptSettings(),
		Pool:           DefaultPoolSettings(),
		WorkBuddy:      DefaultWorkBuddySettings(),
		CodeArts:       DefaultProviderSettings("codearts"),
		Trae:           DefaultProviderSettings("trae"),
		ModelAliases:   map[string]string{},
		DefaultModel:   codearts.DefaultModel,
		RequestTimeout: 300,
		MaxRetries:     3,
		LogKeep:        500,
		// 默认仅放行本机来源；如需局域网访问，在控制台显式配置。
		CORSOrigin:        "",
		AutoRefresh:       true,
		AllowCreditModels: true,
		// 限流默认开启并给保守值：正常单客户端（串行对话 / 轻量 Agent 循环）
		// 用不到这个量级，失控时（循环重试 / 死循环）立刻生效。
		RateLimitEnabled:     true,
		RateLimitQPS:         5,
		RateLimitBurst:       10,
		RateLimitConcurrency: 8,
		AutoClaim:            true,
		ClaimAfterHour:       0,
		ClaimAfterMinute:     5,
		WriteBackToIDE:       true,
		LiveTTLSeconds:       10,
		PluginName:           codearts.PluginName,
		PluginVersion:        codearts.PluginVersion,
		IDEName:              codearts.IDEName,
		IDEVersion:           codearts.IDEVersion,
		Language:             "zh-cn",
	}
}

// Open 载入（或初始化）状态文件。
func Open(path string) (*Store, error) {
	s := &Store{path: path, st: &State{Settings: DefaultSettings()}}
	migrated := false
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s.st); err != nil {
			return nil, fmt.Errorf("状态文件解析失败: %w", err)
		}
		migrated = s.st.Version != StateVersion
		if err := s.migrate(); err != nil {
			return nil, err
		}
		s.applyDefaults()
		s.usageBaseReady = true
		// 收紧已存在状态文件的权限：里面是 STS 凭据与 refresh_token。
		// 旧版本或手工拷贝来的文件可能是 0644，这里纠正一次（Windows 上只影响只读属性）。
		_ = os.Chmod(path, 0o600)
	case errors.Is(err, os.ErrNotExist):
		s.st.Version = StateVersion
		s.st.Stats.StartedAt = time.Now().Unix()
	default:
		return nil, err
	}
	checkpoint := s.st.UsageSequence
	if err := s.replayUsageJournal(); err != nil {
		return nil, fmt.Errorf("恢复用量账本失败: %w", err)
	}
	if migrated || s.st.UsageSequence != checkpoint || s.st.Settings.AdminPassword != "" {
		if err := s.Save(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// migrate 把历史状态文件升级到 StateVersion。
//
// 目前只有 v0（加版本号之前）→ v1：结构未变，仅补上 version 字段。
// 以后改结构时在这里追加 case，保证老文件能平滑升级。
func (s *Store) migrate() error {
	if s.st.Version > StateVersion {
		return fmt.Errorf("状态文件版本 %d 高于本程序支持的 %d，请升级 codearts2api",
			s.st.Version, StateVersion)
	}
	for s.st.Version < StateVersion {
		switch s.st.Version {
		case 6:
			s.st.Version = 7
		case 5:
			s.st.Version = 6
		case 4:
			// Older binaries must not discard the journal checkpoint.
			s.st.Version = 5
		case 3:
			s.st.Version = 4
		case 0:
			s.st.Version = 1
		case 2:
			s.st.Version = 3
		case 1:
			// Password hashes and budgets must not be silently discarded by an
			// older binary. Plaintext passwords are hashed by Open's Save below.
			s.st.Version = 2
		default:
			return fmt.Errorf("状态文件版本 %d 无法迁移", s.st.Version)
		}
	}
	return nil
}

// applyDefaults 补齐缺失 / 非法字段。
func (s *Store) applyDefaults() {
	def := DefaultSettings()
	st := &s.st.Settings
	if st.CommonPrompt.Validate() != nil {
		st.CommonPrompt = def.CommonPrompt
	}
	if st.CodeArts.ValidateFor("codearts") != nil {
		st.CodeArts = def.CodeArts
	}
	if st.Trae.ValidateFor("trae") != nil {
		st.Trae = def.Trae
	}
	if st.WorkBuddy.ValidateFor("workbuddy") != nil {
		st.WorkBuddy = def.WorkBuddy
	}
	if st.Pool.Validate() != nil {
		st.Pool = DefaultPoolSettings()
	}
	if st.ModelAliases == nil {
		st.ModelAliases = def.ModelAliases
	}
	if st.DefaultModel == "" {
		st.DefaultModel = def.DefaultModel
	}
	// 免费权益通道下线后的老状态文件里存的还是 glm-5.3-flash 这类已不存在的模型名，
	// 启动时直接迁移到当前的默认模型。
	if legacyDefaultModels[st.DefaultModel] {
		st.DefaultModel = def.DefaultModel
	}
	// 强制模型优先级最高：它若还是死模型名，每个请求都会 404。
	if legacyDefaultModels[st.ForceModel] {
		st.ForceModel = ""
	}
	if st.RequestTimeout <= 0 {
		st.RequestTimeout = def.RequestTimeout
	}
	if st.MaxRetries <= 0 {
		st.MaxRetries = def.MaxRetries
	}
	if st.LogKeep <= 0 {
		st.LogKeep = def.LogKeep
	}
	// 限流参数留零值时用默认值：老状态文件没有这几个字段，
	// 从 DefaultSettings() 起手 unmarshal 能保住 bool，但 int 需要在这里兜底。
	if st.RateLimitQPS <= 0 {
		st.RateLimitQPS = def.RateLimitQPS
	}
	if st.RateLimitBurst <= 0 {
		st.RateLimitBurst = def.RateLimitBurst
	}
	if st.RateLimitConcurrency < 0 {
		st.RateLimitConcurrency = def.RateLimitConcurrency
	}
	if st.LiveTTLSeconds <= 0 {
		st.LiveTTLSeconds = def.LiveTTLSeconds
	}
	if st.ClaimAfterHour < 0 || st.ClaimAfterHour > 23 {
		st.ClaimAfterHour = def.ClaimAfterHour
	}
	if st.ClaimAfterMinute < 0 || st.ClaimAfterMinute > 59 {
		st.ClaimAfterMinute = def.ClaimAfterMinute
	}
	// 回调端口要么不固定（0），要么落在门户要求的区间内
	if st.LoginPort != 0 && (st.LoginPort < 10000 || st.LoginPort > 65535) {
		st.LoginPort = 0
	}
	if st.PluginName == "" {
		st.PluginName = def.PluginName
	}
	if st.PluginVersion == "" {
		st.PluginVersion = def.PluginVersion
	}
	if st.IDEName == "" {
		st.IDEName = def.IDEName
	}
	if st.IDEVersion == "" {
		st.IDEVersion = def.IDEVersion
	}
	if st.Language == "" {
		st.Language = def.Language
	}
	if st.CORSOrigin == "*" {
		// 历史状态文件里的通配符不再生效，收紧为本机来源。
		st.CORSOrigin = ""
	}
	if s.st.Stats.StartedAt == 0 {
		s.st.Stats.StartedAt = time.Now().Unix()
	}
}
