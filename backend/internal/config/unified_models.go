package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

const MaxModelRevision uint64 = 9007199254740991
const MaxUnifiedModels = 16384

type ModelError struct {
	Status          int
	Reason, Message string
}

func (e *ModelError) Error() string             { return e.Message }
func (e *ModelError) NonRetryableRequest() bool { return true }
func ModelFailure(status int, reason, message string) error {
	return &ModelError{status, reason, message}
}

type ModelSource struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
type UnifiedModel struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Enabled  bool          `json:"enabled"`
	Revision uint64        `json:"revision"`
	Sources  []ModelSource `json:"sources"`
}
type UnifiedSnapshot struct {
	Revision uint64
	Models   []UnifiedModel
	Policies map[string]ModelPolicy
}

var unifiedID = regexp.MustCompile(`^um_[0-9a-f]{32}$`)

func ValidUnifiedID(id string) bool     { return unifiedID.MatchString(id) }
func UnifiedPolicyKey(id string) string { return "unified/" + id }
func ValidModelRevision(v uint64) bool  { return v > 0 && v <= MaxModelRevision }
func ValidateUnifiedName(name string) error {
	if name == "" || len(name) > 200 || strings.Contains(name, "/") || strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ModelFailure(400, "invalid_model_definition", "统一模型名称不能为空，且不能包含空白或斜线")
	}
	return nil
}

// SourcePartition validates identifiers without guessing an upstream model.
func SourcePartition(id string) (string, error) {
	if len(id) > 256 || strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", fmt.Errorf("无效来源 ID")
	}
	p := strings.SplitN(id, "/", 3)
	if len(p) >= 2 && p[0] == "codearts" && strings.TrimPrefix(id, "codearts/") != "" {
		return "codearts", nil
	}
	if len(p) != 3 || p[1] == "" || p[2] == "" {
		return "", fmt.Errorf("来源须为完整平台型号")
	}
	switch p[0] {
	case "workbuddy", "trae", "qoder", "kiro", "chatgpt", "claude", "teleagent":
		return p[0] + "/" + p[1], nil
	}
	return "", fmt.Errorf("未知来源平台")
}
func CanonicalSource(id string) string {
	if !strings.Contains(id, "/") {
		return "codearts/" + id
	}
	return id
}
func RawSource(id string) string { return strings.TrimPrefix(id, "codearts/") }

func (m UnifiedModel) Validate() error {
	if !ValidUnifiedID(m.ID) || !ValidModelRevision(m.Revision) {
		return ModelFailure(400, "invalid_model_definition", "统一模型 ID 或版本无效")
	}
	if err := ValidateUnifiedName(m.Name); err != nil {
		return err
	}
	if len(m.Sources) < 1 || len(m.Sources) > 16 {
		return ModelFailure(400, "invalid_model_definition", "统一模型须有 1–16 个来源")
	}
	seen := map[string]bool{}
	for _, s := range m.Sources {
		p, err := SourcePartition(s.ID)
		if err != nil || seen[p] {
			return ModelFailure(400, "invalid_model_definition", "来源无效或平台地区重复")
		}
		seen[p] = true
	}
	return nil
}
func CloneUnifiedModel(m UnifiedModel) UnifiedModel {
	m.Sources = append([]ModelSource{}, m.Sources...)
	return m
}
func CloneUnifiedModels(in []UnifiedModel) []UnifiedModel {
	out := make([]UnifiedModel, len(in))
	for i, m := range in {
		out[i] = CloneUnifiedModel(m)
	}
	return out
}

// StrictModelJSON rejects missing/null fields and unknown input before bool
// zero values could turn an incomplete permission record into a valid record.
func StrictModelJSON(raw []byte, dst any, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("模型配置须为 JSON 对象")
	}
	for _, name := range required {
		v, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("模型字段缺失或为 null: %s", name)
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("多余 JSON 内容")
	}
	return nil
}
func (m *UnifiedModel) UnmarshalJSON(raw []byte) error {
	type plain UnifiedModel
	return StrictModelJSON(raw, (*plain)(m), "id", "name", "enabled", "revision", "sources")
}
func (s *ModelSource) UnmarshalJSON(raw []byte) error {
	type plain ModelSource
	return StrictModelJSON(raw, (*plain)(s), "id", "enabled")
}

func ValidateUnifiedPolicy(p ModelPolicy, sources []ModelSource, lookup func(string) (ModelPolicy, bool)) error {
	if err := p.Validate(); err != nil {
		return ModelFailure(409, "model_policy_conflict", err.Error())
	}
	if p.SummaryEnabled || p.SummaryRetry || p.SummaryFallbackModel != "" || p.DisableSummaryThinking {
		return ModelFailure(409, "model_policy_conflict", "统一模型不启用平台专用摘要处理")
	}
	count := 0
	for _, src := range sources {
		if !src.Enabled {
			continue
		}
		count++
		q, ok := lookup(RawSource(src.ID))
		if !ok || p.ContextWindow > q.ContextWindow || p.MaxOutputTokens > q.MaxOutputTokens {
			return ModelFailure(409, "model_policy_conflict", "统一策略缺少来源依据或超过来源限制")
		}
	}
	if count == 0 {
		return ModelFailure(409, "model_policy_conflict", "没有启用来源可验证客户端策略")
	}
	return nil
}
