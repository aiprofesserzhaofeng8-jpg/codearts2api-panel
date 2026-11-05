package config

import (
	"errors"
	"strings"
)

// ErrModelPolicySave lets HTTP retain its existing persistence-error response.
var ErrModelPolicySave = errors.New("保存模型策略失败")

func (m *Management) SaveModelPolicy(requested string, policy ModelPolicy) error {
	return m.saveModelPolicy(requested, policy, 0, nil, false)
}

func (m *Management) SaveModelPolicyChecked(requested string, policy ModelPolicy, revision uint64, sourcePolicies map[string]ModelPolicy) error {
	return m.saveModelPolicy(requested, policy, revision, sourcePolicies, true)
}

func (m *Management) saveModelPolicy(requested string, policy ModelPolicy, revision uint64, sourcePolicies map[string]ModelPolicy, checked bool) error {
	model, ok := m.resolve(requested)
	if !ok {
		return errors.New("模型未在当前目录中")
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	fallback := strings.TrimSpace(policy.SummaryFallbackModel)
	if fallback != "" {
		target, exists := m.resolve(fallback)
		if !exists || target.ID == model.ID || target.Provider != model.Provider || target.Scope != model.Scope {
			return errors.New("摘要备用模型须为相同上游和地区的其他模型")
		}
		if forced := m.store.Settings().ForceModel; forced != "" && forced != target.ID {
			return errors.New("请先取消强制模型，再配置其他摘要备用模型")
		}
		policy.SummaryFallbackModel = target.ID
	}
	if model.Provider != "codearts" && policy.SummaryEnabled {
		return errors.New("当前摘要完整性适配仅支持码道；其他上游可配置上下文建议，但请关闭服务端摘要保护")
	}
	policy.Source = "管理员配置，非上游能力声明"
	if checked {
		st, ok := m.store.(interface {
			SetModelPolicyWithSources(string, ModelPolicy, uint64, map[string]ModelPolicy) error
		})
		if !ok {
			return ModelFailure(500, "model_save_failed", "模型策略事务不可用")
		}
		return st.SetModelPolicyWithSources(model.ID, policy, revision, sourcePolicies)
	}
	if err := m.store.SetModelPolicyAndSave(model.ID, policy); err != nil {
		return ErrModelPolicySave
	}
	return nil
}
