package catalog

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

//go:embed default_models.json
var defaultModelsJSON []byte

type defaultCatalogEntry struct {
	Name        string   `json:"name"`
	LegacyNames []string `json:"legacy_names"`
	Providers   []string `json:"providers"`
	UpstreamIDs []string `json:"upstream_ids"`
}
type qoderModelAlias struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Basis       string `json:"basis"`
}
type defaultCatalogData struct {
	QoderCNAliases []qoderModelAlias              `json:"qoder_cn_aliases"`
	Version        uint64                         `json:"version"`
	Models         []defaultCatalogEntry          `json:"models"`
	NativeModels   []provider.NativeModelContract `json:"native_models"`
}

var defaultCatalog = func() defaultCatalogData {
	var value defaultCatalogData
	if err := json.Unmarshal(defaultModelsJSON, &value); err != nil {
		panic(err)
	}
	return value
}()

type DefaultModelEntry struct {
	Name       string               `json:"name"`
	Sources    []config.ModelSource `json:"sources"`
	Status     string               `json:"status"`
	Message    string               `json:"message"`
	ExistingID string               `json:"existing_id"`
}
type DefaultModelsPreview struct {
	Version         uint64              `json:"version"`
	CatalogRevision string              `json:"catalog_revision"`
	RoutingRevision uint64              `json:"routing_revision"`
	Providers       []string            `json:"providers"`
	Models          []DefaultModelEntry `json:"models"`
}

// Model identities use complete upstream IDs, preserving all version/variant
// suffixes. Internal provider routes (auto, dfmodel, custom_model_*) stay scoped.
var publicModelIdentity = regexp.MustCompile(`(?i)^(?:deepseek-(?:v?[0-9]|r[0-9])|glm-[0-9]|kimi-k[0-9]|qwen-?[0-9]|gpt-(?:[0-9]|oss-[0-9])|claude-(?:[0-9]|(?:haiku|sonnet|opus)-[0-9])|gemini-[0-9]|minimax-m[0-9]|mimo-v[0-9]|doubao-seed-[0-9]|step-[0-9]|openpangu-[0-9]|hunyuan-[0-9]|moonshot-v[0-9]|o[1-9](?:-|$))`)

func modelIdentity(raw provider.Model) (string, bool) {
	_, _, upstream := provider.ParseModel(raw.ID)
	if publicModelIdentity.MatchString(upstream) && !strings.ContainsAny(upstream, " /\t\r\n") {
		return strings.ToLower(upstream), true
	}
	return "", false
}
func qoderCNAlias(id string) (qoderModelAlias, bool) {
	p, region, upstream := provider.ParseModel(id)
	if p != provider.Qoder || region != "cn" {
		return qoderModelAlias{}, false
	}
	for _, alias := range defaultCatalog.QoderCNAliases {
		if alias.ID == upstream {
			return alias, true
		}
	}
	return qoderModelAlias{}, false
}
func defaultModelName(raw provider.Model) (string, []string) {
	if alias, ok := qoderCNAlias(raw.ID); ok && strings.EqualFold(strings.TrimSpace(raw.Name), alias.DisplayName) {
		return alias.Name, nil
	}
	p, _, upstream := provider.ParseModel(raw.ID)
	for _, entry := range defaultCatalog.Models {
		for _, allowed := range entry.Providers {
			if allowed != p {
				continue
			}
			for _, id := range entry.UpstreamIDs {
				if strings.EqualFold(id, upstream) {
					return entry.Name, entry.LegacyNames
				}
			}
		}
	}
	if name, known := modelIdentity(raw); known {
		return name, nil
	}
	return providerSpecificName(raw), nil
}

func providerSpecificName(raw provider.Model) string {
	p, _, upstream := provider.ParseModel(raw.ID)
	name := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '/' {
			return '-'
		}
		return r
	}, upstream)
	if name != upstream {
		h := sha256.Sum256([]byte(upstream))
		name += "-" + hex.EncodeToString(h[:6])
	}
	name = p + "-" + name
	if len(name) > 200 {
		h := sha256.Sum256([]byte(raw.ID))
		name = p + "-model-" + hex.EncodeToString(h[:12])
	}
	return name
}

// Earlier defaults emitted one provider-prefixed public definition per source.
// Keep these definitions callable, but identify redundant entries for the public
// picker once a canonical model covers their sources. Handwritten names and
// custom policies are not treated as generated compatibility entries.
func compatibilityModelTargets(snapshot config.UnifiedSnapshot) map[string]config.UnifiedModel {
	byName := map[string]config.UnifiedModel{}
	for _, m := range snapshot.Models {
		byName[strings.ToLower(m.Name)] = m
	}
	out := map[string]config.UnifiedModel{}
	for _, old := range snapshot.Models {
		if _, custom := snapshot.Policies[config.UnifiedPolicyKey(old.ID)]; custom {
			continue
		}
		targetName := ""
		generated := len(old.Sources) > 0
		for _, src := range old.Sources {
			raw := provider.Model{ID: config.RawSource(src.ID)}
			name, known := modelIdentity(raw)
			if alias, ok := qoderCNAlias(raw.ID); ok {
				name, known = alias.Name, true
			}
			if !known || old.Name != providerSpecificName(raw) || (targetName != "" && name != targetName) {
				generated = false
				break
			}
			targetName = name
		}
		target, ok := byName[targetName]
		if !generated || !ok || target.ID == old.ID || !target.Enabled {
			continue
		}
		covered := true
		for _, src := range old.Sources {
			found := false
			for _, next := range target.Sources {
				if next.ID == src.ID && next.Enabled == src.Enabled {
					found = true
					break
				}
			}
			if !found {
				covered = false
				break
			}
		}
		if covered {
			out[old.ID] = target
		}
	}
	return out
}
func (m *Management) CompatibilityAliases(snapshot config.UnifiedSnapshot) map[string]string {
	out := map[string]string{}
	for id, target := range compatibilityModelTargets(snapshot) {
		out[id] = target.Name
	}
	return out
}

func (m *Management) defaultModelsPlan() (DefaultModelsPreview, map[string]config.UnifiedModel, map[string]config.ModelPolicy, error) {
	snap := m.store.UnifiedSnapshot()
	settings := m.store.Settings()
	rows := m.directory.List(nil)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	digestData, _ := json.Marshal(rows)
	digest := sha256.Sum256(digestData)
	out := DefaultModelsPreview{Version: defaultCatalog.Version, CatalogRevision: hex.EncodeToString(digest[:]), RoutingRevision: snap.Revision,
		Providers: []string{provider.CodeArts, provider.WorkBuddy, provider.Trae, provider.Qoder, provider.Kiro, provider.ChatGPT, provider.Claude, provider.TeleAgent}, Models: []DefaultModelEntry{}}
	proposals := map[string]config.UnifiedModel{}
	policies := map[string]config.ModelPolicy{}
	existing := map[string]config.UnifiedModel{}
	for _, value := range snap.Models {
		existing[strings.ToLower(value.Name)] = value
	}
	messages := map[string]string{}
	for _, raw := range rows {
		if raw.Source == "alias" {
			continue
		}
		source := config.CanonicalSource(raw.ID)
		if _, err := config.SourcePartition(source); err != nil {
			continue
		}
		name, legacy := defaultModelName(raw)
		old, found := existing[strings.ToLower(name)]
		if !found {
			for _, alias := range legacy {
				if v, ok := existing[strings.ToLower(alias)]; ok {
					old, found = v, true
					break
				}
			}
		}
		if found {
			name = old.Name
		}
		value, exists := proposals[name]
		if !exists {
			value = config.UnifiedModel{Name: name, Enabled: true, Sources: []config.ModelSource{}}
			if found {
				value = config.CloneUnifiedModel(old)
			}
		}
		present := false
		for _, src := range value.Sources {
			if src.ID == source {
				present = true
			}
		}
		if !present {
			enabled := true
			if oldGenerated, ok := existing[strings.ToLower(providerSpecificName(raw))]; ok && oldGenerated.ID != value.ID {
				for _, oldSource := range oldGenerated.Sources {
					if oldSource.ID == source {
						enabled = oldGenerated.Enabled && oldSource.Enabled
						break
					}
				}
			}
			value.Sources = append(value.Sources, config.ModelSource{ID: source, Enabled: enabled})
		}
		proposals[name] = value
		if m.policies != nil {
			if p, ok := m.policies.For(raw); ok {
				policies[raw.ID] = p
			}
		}
		if settings.ForceModel != "" {
			messages[name] = "请先关闭强制型号"
		}
		for alias := range settings.ModelAliases {
			if strings.EqualFold(alias, name) {
				messages[name] = "与自定义别名冲突，请先处理该别名"
			}
		}
	}
	if len(proposals) > 512 {
		return out, nil, nil, config.ModelFailure(400, "invalid_model_definition", "默认目录超过 512 个型号，请使用手动管理")
	}
	names := make([]string, 0, len(proposals))
	for name := range proposals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := proposals[name]
		check := value
		if check.ID == "" {
			check.ID = "um_00000000000000000000000000000000"
			check.Revision = 1
		}
		status := "create"
		if value.ID != "" {
			status = "update"
			old := existing[strings.ToLower(value.Name)]
			if len(old.Sources) == len(value.Sources) {
				status = "unchanged"
			}
		}
		if err := check.Validate(); err != nil {
			messages[name] = err.Error()
		}
		if policy, ok := snap.Policies[config.UnifiedPolicyKey(value.ID)]; ok {
			if err := config.ValidateUnifiedPolicy(policy, value.Sources, func(id string) (config.ModelPolicy, bool) {
				if p, ok := snap.Policies[id]; ok {
					return p, true
				}
				if p, ok := config.DefaultModelPolicies()[id]; ok {
					return p, true
				}
				p, ok := policies[id]
				return p, ok
			}); err != nil {
				messages[name] = "新增来源与已保存的客户端策略不兼容，请手动编辑"
			}
		}
		if messages[name] != "" {
			status = "conflict"
		}
		out.Models = append(out.Models, DefaultModelEntry{Name: name, Sources: value.Sources, Status: status, Message: messages[name], ExistingID: value.ID})
	}
	return out, proposals, policies, nil
}
func (m *Management) DefaultModels() (DefaultModelsPreview, error) {
	p, _, _, err := m.defaultModelsPlan()
	return p, err
}
func (m *Management) ApplyDefaultModels(request config.DefaultModelsApply) (config.UnifiedSnapshot, error) {
	preview, proposals, policies, err := m.defaultModelsPlan()
	if err != nil {
		return config.UnifiedSnapshot{}, err
	}
	fail := func(message string) (config.UnifiedSnapshot, error) {
		return config.UnifiedSnapshot{}, config.ModelFailure(409, "revision_conflict", message)
	}
	if request.ExpectedRoutingRevision == 0 {
		return config.UnifiedSnapshot{}, config.ModelFailure(428, "revision_required", "缺少配置版本条件")
	}
	if request.Version != preview.Version || request.CatalogRevision != preview.CatalogRevision || request.ExpectedRoutingRevision != preview.RoutingRevision {
		return fail("默认目录或配置已变化，请重新预览")
	}
	if len(request.Names) == 0 || len(request.Names) > 512 {
		return config.UnifiedSnapshot{}, config.ModelFailure(400, "invalid_model_definition", "请选择 1–512 个默认模型")
	}
	conflicts := map[string]string{}
	for _, entry := range preview.Models {
		if entry.Status == "conflict" {
			conflicts[entry.Name] = entry.Message
		}
	}
	models := []config.UnifiedModel{}
	seen := map[string]bool{}
	for _, name := range request.Names {
		value, ok := proposals[name]
		if !ok || seen[name] {
			return config.UnifiedSnapshot{}, config.ModelFailure(400, "invalid_model_definition", "默认模型不存在或重复")
		}
		if message := conflicts[name]; message != "" {
			return config.UnifiedSnapshot{}, config.ModelFailure(409, "model_name_conflict", fmt.Sprintf("%s：%s", name, message))
		}
		seen[name] = true
		models = append(models, value)
	}
	st, ok := m.store.(interface {
		ApplyDefaultModels([]config.UnifiedModel, uint64, string, uint64, map[string]config.ModelPolicy) (config.UnifiedSnapshot, error)
	})
	if !ok {
		return config.UnifiedSnapshot{}, fmt.Errorf("default model storage unavailable")
	}
	return st.ApplyDefaultModels(models, request.ExpectedRoutingRevision, request.GroupID, request.ExpectedGroupRevision, policies)
}
