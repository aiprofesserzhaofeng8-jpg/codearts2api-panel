package config

import (
	"codearts2api/internal/access"
	"encoding/json"
	"errors"
	"strings"
)

type ManagementStore interface {
	Settings() Settings
	Groups() []access.AccountGroup
	UpdateSettings(func(*Settings))
	SetModelPolicyAndSave(string, ModelPolicy) error
	Save() error
}

// ModelRef borrows identity only; catalog ownership remains unchanged.
type ModelRef struct{ ID, Provider, Scope string }
type Management struct {
	store   ManagementStore
	resolve func(string) (ModelRef, bool)
	logf    func(string, ...any)
}

func NewManagement(store ManagementStore, resolve func(string) (ModelRef, bool), logf func(string, ...any)) *Management {
	return &Management{store: store, resolve: resolve, logf: logf}
}
func (m *Management) Settings() Settings { return m.store.Settings() }

// UpdateSettings retains validation-before-mutation and legacy logged Save failure.
// HTTP applies the existing identity and limiter effects after this returns.
func (m *Management) UpdateSettings(body SettingsUpdate) error {
	if body.ForceModel != nil && *body.ForceModel != "" {
		for _, g := range m.store.Groups() {
			if len(g.ModelMappings) > 0 {
				return errors.New("分组统一模型启用时不能设置强制模型")
			}
		}
	}
	current := m.Settings()
	common := current.CommonPrompt
	if len(body.CommonPrompt) > 0 {
		if string(body.CommonPrompt) == "null" || json.Unmarshal(body.CommonPrompt, &common) != nil {
			return errors.New("公共提示词格式错误")
		}
		if err := common.Validate(); err != nil {
			return err
		}
	}
	policies := map[string]ProviderSettings{}
	for name, raw := range map[string]json.RawMessage{"workbuddy": body.WorkBuddy, "codearts": body.CodeArts, "trae": body.Trae, "qoder": body.Qoder, "kiro": body.Kiro, "claude": body.Claude, "chatgpt": body.ChatGPT, "teleagent": body.TeleAgent} {
		if len(raw) == 0 {
			continue
		}
		config := current.ConfiguredProvider(name).Clone()
		// Overrides are a replacement object: {} means inherit all, not a merge
		// with the previous optional fields. Other legacy platform fields PATCH.
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) == nil {
			if _, present := fields["prompt_overrides"]; present {
				config.PromptOverrides = nil
			}
		}

		if string(raw) == "null" || json.Unmarshal(raw, &config) != nil {
			return errors.New(name + " 配置格式错误")
		}
		if err := config.ValidateFor(name); err != nil {
			return err
		}
		if err := config.ResolvePrompt(common).ValidateFor(name); err != nil {
			return err
		}
		policies[name] = config
	}
	for _, name := range []string{"codearts", "trae", "workbuddy", "qoder", "kiro", "claude", "chatgpt", "teleagent"} {
		candidate, ok := policies[name]
		if !ok {
			candidate = current.ConfiguredProvider(name)
			// Older in-memory settings may predate the additive provider field.
			if (name == "qoder" || name == "kiro" || name == "chatgpt" || name == "claude" || name == "teleagent") && candidate.PromptMode == "" {
				candidate = DefaultProviderSettings(name)
			}
		}
		if err := candidate.ResolvePrompt(common).ValidateFor(name); err != nil {
			return err
		}
	}
	poolConfig := m.Settings().Pool
	if len(body.Pool) > 0 {
		if err := json.Unmarshal(body.Pool, &poolConfig); err != nil {
			return errors.New("账号池配置格式错误")
		}
		if err := poolConfig.Validate(); err != nil {
			return err
		}
	}
	if body.ClearPassword != nil && *body.ClearPassword {
		return errors.New("默认登录模式不允许清空密码；可设置新密码，或由启动参数重设")
	}
	if body.AdminPassword != nil && len(*body.AdminPassword) > 4096 {
		return errors.New("管理员密码不能超过 4096 字节")
	}
	m.store.UpdateSettings(func(st *Settings) {
		if len(body.CommonPrompt) > 0 {
			st.CommonPrompt = common
		}
		if config, ok := policies["workbuddy"]; ok {
			st.WorkBuddy = config
		}
		if config, ok := policies["codearts"]; ok {
			st.CodeArts = config
		}
		if config, ok := policies["trae"]; ok {
			st.Trae = config
		}
		if config, ok := policies["qoder"]; ok {
			st.Qoder = config
		}
		if config, ok := policies["kiro"]; ok {
			st.Kiro = config
		}
		if config, ok := policies["claude"]; ok {
			st.Claude = config
		}
		if config, ok := policies["chatgpt"]; ok {
			st.ChatGPT = config
		}
		if config, ok := policies["teleagent"]; ok {
			st.TeleAgent = config
		}
		if len(body.Pool) > 0 {
			st.Pool = poolConfig
		}
		setIf(&st.AutoClaim, body.AutoClaim)
		setIf(&st.ClaimAfterHour, body.ClaimAfterHour)
		setIf(&st.ClaimAfterMinute, body.ClaimAfterMinute)
		if body.ModelAliases != nil {
			st.ModelAliases = body.ModelAliases
		}
		setIf(&st.DefaultModel, body.DefaultModel)
		setIf(&st.ForceModel, body.ForceModel)
		setIf(&st.RequestTimeout, body.RequestTimeout)
		setIf(&st.MaxRetries, body.MaxRetries)
		setIf(&st.LogKeep, body.LogKeep)
		setIf(&st.CORSOrigin, body.CORSOrigin)
		setIf(&st.AutoRefresh, body.AutoRefresh)
		setIf(&st.AllowCreditModels, body.AllowCreditModels)
		setIf(&st.AllowAnonymous, body.AllowAnonymous)
		setIf(&st.RateLimitEnabled, body.RateLimitEnabled)
		setIf(&st.RateLimitQPS, body.RateLimitQPS)
		setIf(&st.RateLimitBurst, body.RateLimitBurst)
		setIf(&st.RateLimitConcurrency, body.RateLimitConc)
		setIf(&st.WriteBackToIDE, body.WriteBackToIDE)
		setIf(&st.LiveTTLSeconds, body.LiveTTLSeconds)
		setIf(&st.UserDataDir, body.UserDataDir)
		setIf(&st.LoginPort, body.LoginPort)
		setIf(&st.PluginName, body.PluginName)
		setIf(&st.PluginVersion, body.PluginVersion)
		setIf(&st.IDEName, body.IDEName)
		setIf(&st.IDEVersion, body.IDEVersion)
		setIf(&st.Language, body.Language)
		// Empty fields leave the existing administrator credential unchanged.
		if body.AdminPassword != nil {
			if v := strings.TrimSpace(*body.AdminPassword); v != "" {
				st.AdminPassword = v
			}
		}
	})
	if err := m.store.Save(); err != nil && m.logf != nil {
		m.logf("保存状态文件失败: %v", err)
	}
	return nil
}

// setIf 仅在指针非空时赋值。
func setIf[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}
