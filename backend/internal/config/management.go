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
	UpdateSettingsAndSave(func(*Settings)) error
	SetModelPolicyAndSave(string, ModelPolicy) error
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

// UpdateSettings returns success only after the validated settings reach storage.
// HTTP applies the existing identity and limiter effects after this returns.
func (m *Management) UpdateSettings(body SettingsUpdate) error {
	return m.updateSettings(body, false)
}

func (m *Management) UpdateSettingsChecked(body SettingsUpdate) error {
	return m.updateSettings(body, true)
}

func (m *Management) updateSettings(body SettingsUpdate, checked bool) error {
	routing := body.ModelAliases != nil || body.DefaultModel != nil || body.ForceModel != nil
	if !checked && body.ForceModel != nil && *body.ForceModel != "" {
		for _, g := range m.store.Groups() {
			if len(g.ModelMappings) > 0 {
				return errors.New("分组统一模型启用时不能设置强制模型")
			}
		}
	}
	current := m.Settings()
	if checked && body.ModelAliases != nil {
		for alias, target := range body.ModelAliases {
			if strings.TrimSpace(alias) == "" || len(alias) > 200 || strings.TrimSpace(target) != target || target == "" {
				return ModelFailure(400, "invalid_model_definition", "模型别名无效")
			}
			for name := range body.ModelAliases {
				if strings.EqualFold(name, target) {
					return ModelFailure(409, "model_name_conflict", "别名不能引用其他别名")
				}
			}
			model, ok := m.resolve(target)
			if !ok || (model.ID != target && "codearts/"+model.ID != target) {
				return ModelFailure(409, "model_name_conflict", "别名须直接指向原始型号")
			}
		}
	}
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
	if checked {
		for _, name := range []*string{body.DefaultModel, body.ForceModel} {
			if name == nil || *name == "" {
				continue
			}
			_, found := m.resolve(*name)
			if st, ok := m.store.(interface{ UnifiedSnapshot() UnifiedSnapshot }); ok {
				for _, model := range st.UnifiedSnapshot().Models {
					if model.Name == *name {
						found = true
					}
				}
			}
			if !found {
				return ModelFailure(404, "model_not_found", "默认或强制型号不存在")
			}
		}
	}
	update := func(st *Settings) {
		if len(body.CommonPrompt) > 0 {
			st.CommonPrompt = common
		}
		if config, ok := policies["workbuddy"]; ok {
			st.WorkBuddy = mergeProviderPatch(st.WorkBuddy, config, body.WorkBuddy)
		}
		if config, ok := policies["codearts"]; ok {
			st.CodeArts = mergeProviderPatch(st.CodeArts, config, body.CodeArts)
		}
		if config, ok := policies["trae"]; ok {
			st.Trae = mergeProviderPatch(st.Trae, config, body.Trae)
		}
		if config, ok := policies["qoder"]; ok {
			st.Qoder = mergeProviderPatch(st.Qoder, config, body.Qoder)
		}
		if config, ok := policies["kiro"]; ok {
			st.Kiro = mergeProviderPatch(st.Kiro, config, body.Kiro)
		}
		if config, ok := policies["claude"]; ok {
			st.Claude = mergeProviderPatch(st.Claude, config, body.Claude)
		}
		if config, ok := policies["chatgpt"]; ok {
			st.ChatGPT = mergeProviderPatch(st.ChatGPT, config, body.ChatGPT)
		}
		if config, ok := policies["teleagent"]; ok {
			st.TeleAgent = mergeProviderPatch(st.TeleAgent, config, body.TeleAgent)
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
	}
	if checked && routing {
		st, ok := m.store.(interface {
			UpdateSettingsChecked(uint64, func(*Settings)) error
		})
		if !ok {
			return ModelFailure(500, "model_save_failed", "模型设置事务不可用")
		}
		return st.UpdateSettingsChecked(body.ExpectedRoutingRevision, update)
	}
	return m.store.UpdateSettingsAndSave(update)
}

// setIf 仅在指针非空时赋值。
func setIf[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

// Keep independent editors from clobbering fields absent from their PATCH.
func mergeProviderPatch(current, validated ProviderSettings, raw json.RawMessage) ProviderSettings {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if _, ok := fields["prompt_mode"]; !ok {
		validated.PromptMode = current.PromptMode
	}
	if _, ok := fields["prompt_text"]; !ok {
		validated.PromptText = current.PromptText
	}
	if _, ok := fields["prompt_fallback"]; !ok {
		validated.PromptFallback = current.PromptFallback
	}
	if _, ok := fields["sanitize"]; !ok {
		validated.Sanitize = current.Sanitize
	}
	if _, ok := fields["prompt_overrides"]; !ok {
		validated.PromptOverrides = current.Clone().PromptOverrides
	}
	if _, ok := fields["search_enabled"]; !ok {
		validated.SearchEnabled = current.SearchEnabled
	}
	if _, ok := fields["search_scope"]; !ok {
		validated.SearchScope = current.SearchScope
	}
	if _, ok := fields["search_backend"]; !ok {
		validated.SearchBackend = current.SearchBackend
	}
	if _, ok := fields["search_url"]; !ok {
		validated.SearchURL = current.SearchURL
	}
	return validated
}
