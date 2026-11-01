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
func (m *Management) UpdateSettings(body SettingsUpdate, allowClearPassword bool) error {
	if body.ForceModel != nil && *body.ForceModel != "" {
		for _, g := range m.store.Groups() {
			if len(g.ModelMappings) > 0 {
				return errors.New("分组统一模型启用时不能设置强制模型")
			}
		}
	}
	current := m.Settings()
	policies := map[string]ProviderSettings{}
	for name, raw := range map[string]json.RawMessage{"workbuddy": body.WorkBuddy, "codearts": body.CodeArts, "trae": body.Trae} {
		if len(raw) == 0 {
			continue
		}
		config := current.ProviderPolicy(name)
		if string(raw) == "null" || json.Unmarshal(raw, &config) != nil {
			return errors.New(name + " 配置格式错误")
		}
		if err := config.ValidateFor(name); err != nil {
			return err
		}
		policies[name] = config
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
	if body.ClearPassword != nil && *body.ClearPassword && !allowClearPassword {
		return errors.New("默认登录模式不允许清空密码；可设置新密码，或由启动参数重设")
	}
	if body.AdminPassword != nil && len(*body.AdminPassword) > 4096 {
		return errors.New("管理员密码不能超过 4096 字节")
	}
	m.store.UpdateSettings(func(st *Settings) {
		if config, ok := policies["workbuddy"]; ok {
			st.WorkBuddy = config
		}
		if config, ok := policies["codearts"]; ok {
			st.CodeArts = config
		}
		if config, ok := policies["trae"]; ok {
			st.Trae = config
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
		// 只有显式带 clear_admin_password 才清除：空字符串一律视为「不修改」。
		// 否则误发一个空字段就会把管理密码抹掉，让管理接口退回「本机直连」兜底。
		if body.ClearPassword != nil && *body.ClearPassword {
			st.AdminPassword = ""
		} else if body.AdminPassword != nil {
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
