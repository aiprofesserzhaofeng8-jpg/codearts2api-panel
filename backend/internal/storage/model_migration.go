package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"codearts2api/internal/access"
	"codearts2api/internal/config"
)

type MigrationIssue struct {
	Reason    string   `json:"reason"`
	GroupIDs  []string `json:"group_ids"`
	ModelName string   `json:"model_name"`
	FieldPath string   `json:"field_path"`
	Message   string   `json:"message"`
}
type ModelMigrationReport struct {
	FromSchema         int              `json:"from_schema"`
	ToSchema           int              `json:"to_schema"`
	Ready              bool             `json:"ready"`
	Definitions        int              `json:"definitions"`
	GroupChanges       int              `json:"group_changes"`
	Conflicts          []MigrationIssue `json:"conflicts"`
	Warnings           []MigrationIssue `json:"warnings"`
	RoutingInputSHA256 string           `json:"routing_input_sha256"`
}

func migrationModelID(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "um_" + hex.EncodeToString(sum[:16])
}
func routingDigest(st *State) string {
	// Only routing fields, never credentials, keys or usage.
	v := struct {
		Groups         []AccountGroup
		Models         []config.UnifiedModel
		Aliases        map[string]string
		Default, Force string
	}{st.Groups, st.UnifiedModels, st.Settings.ModelAliases, st.Settings.DefaultModel, st.Settings.ForceModel}
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func convertModelState(st *State) (*State, ModelMigrationReport) {
	r := ModelMigrationReport{FromSchema: st.Version, ToSchema: 11, Conflicts: []MigrationIssue{}, Warnings: []MigrationIssue{}, RoutingInputSHA256: routingDigest(st)}
	issue := func(reason string, g AccountGroup, name, message string) {
		r.Conflicts = append(r.Conflicts, MigrationIssue{reason, []string{g.ID}, name, "groups.model_mappings", message})
	}
	if st.Version == 11 {
		if err := validateUnifiedState(st); err != nil {
			issue("invalid_state", AccountGroup{}, "", err.Error())
		}
		r.Definitions = len(st.UnifiedModels)
		r.Ready = len(r.Conflicts) == 0
		return st, r
	}
	if st.Version != 10 {
		issue("unsupported_schema", AccountGroup{}, "", "仅支持 schema 10 到 11")
		return nil, r
	}
	next := *st
	next.UnifiedModels = []config.UnifiedModel{}
	next.ModelRoutingRevision = 1
	next.Version = 11
	next.Groups = make([]AccountGroup, len(st.Groups))
	names := map[string]int{}
	for i, old := range st.Groups {
		g := cloneGroup(old)
		g.Revision = 1
		g.ExpectedRevision = 0
		g.UnifiedModels = []access.UnifiedGrant{}
		g.RawModelMode = "legacy"
		g.RawModelsVisibleDefault = true
		if len(old.ModelMappings) > 0 {
			g.RawModelMode = "qualified_only"
			g.RawModelsVisibleDefault = false
		}
		if err := validateMappings(old.ModelMappings); err != nil {
			issue("invalid_legacy_mapping", old, "", "旧映射格式无效")
			continue
		}
		public := map[string]bool{}
		for _, mapping := range old.ModelMappings {
			public[mapping.Name] = true
			m := config.UnifiedModel{ID: migrationModelID(mapping.Name), Name: mapping.Name, Enabled: true, Revision: 1, Sources: sortedSources(mapping.Sources)}
			if err := m.Validate(); err != nil {
				issue("invalid_legacy_mapping", old, m.Name, "旧来源 ID 无效")
				continue
			}
			if reservedName(st, m.Name) {
				issue("name_namespace_conflict", old, m.Name, "名称与原始型号或别名冲突")
				continue
			}
			name := strings.ToLower(m.Name)
			if n, ok := names[name]; ok {
				existing := next.UnifiedModels[n]
				if existing.Name != m.Name {
					issue("name_case_conflict", old, m.Name, "公开名称大小写冲突")
					continue
				}
				if !reflect.DeepEqual(existing.Sources, m.Sources) {
					issue("name_sources_conflict", old, m.Name, "同名映射来源不同")
					continue
				}
				m = existing
			} else {
				collision := false
				for _, x := range next.UnifiedModels {
					collision = collision || x.ID == m.ID
				}
				if collision {
					issue("id_collision", old, m.Name, "统一模型 ID 冲突")
					continue
				}
				names[name] = len(next.UnifiedModels)
				next.UnifiedModels = append(next.UnifiedModels, m)
			}
			scope := access.AccessScope{ModelRules: old.ModelRules}
			a := access.UnifiedGrant{ModelID: m.ID, Visible: scope.Visible(m.Name, true), Callable: scope.Callable(m.Name), SourceMode: "selected", SourceIDs: []string{}}
			for _, src := range m.Sources {
				a.SourceIDs = append(a.SourceIDs, src.ID)
			}
			g.UnifiedModels = append(g.UnifiedModels, a)
		}
		g.ModelMappings = nil
		g.ModelRules = nil
		for _, rule := range old.ModelRules {
			if !public[rule.ID] {
				g.ModelRules = append(g.ModelRules, rule)
			}
		}
		next.Groups[i] = g
	}
	if len(next.UnifiedModels) > 0 && st.Settings.ForceModel != "" {
		issue("force_model_conflict", AccountGroup{}, "", "强制模型与统一模型不能共存")
	}
	for _, g := range next.Groups {
		if g.ID == "" {
			issue("invalid_legacy_group", g, "", "旧分组标识缺失")
			continue
		}
		if err := validateUnifiedGroup(g, next.UnifiedModels); err != nil {
			issue("default_reference_conflict", g, "", err.Error())
		}
	}
	if len(next.UnifiedModels) > 0 {
		r.Warnings = append(r.Warnings, MigrationIssue{"catalog_not_verified_offline", []string{}, "", "unified_models", "离线保留原始绑定，未访问上游验证目录和账号可用性"})
	}
	r.Definitions = len(next.UnifiedModels)
	r.GroupChanges = len(next.Groups)
	if len(r.Conflicts) == 0 {
		if err := validateUnifiedState(&next); err != nil {
			issue("invalid_state", AccountGroup{}, "", err.Error())
		}
	}
	r.Ready = len(r.Conflicts) == 0
	if !r.Ready {
		return nil, r
	}
	sort.Slice(next.UnifiedModels, func(i, j int) bool { return next.UnifiedModels[i].ID < next.UnifiedModels[j].ID })
	return &next, r
}

func CheckStateMigration(path string) (ModelMigrationReport, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return ModelMigrationReport{}, fmt.Errorf("无法读取状态文件")
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return ModelMigrationReport{}, fmt.Errorf("状态文件类型或大小无效")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ModelMigrationReport{}, fmt.Errorf("无法读取状态文件")
	}
	var st State
	if err = json.Unmarshal(raw, &st); err != nil {
		return ModelMigrationReport{}, fmt.Errorf("状态 JSON 或必需权限字段无效")
	}
	_, r := convertModelState(&st)
	return r, nil
}

// Compatibility write adapter until U4 replaces group mapping writes. Global
// definitions are the only persisted source; this view is not saved twice.
func (s *Store) importLegacyGroupLocked(g AccountGroup) (AccountGroup, error) {
	g.UnifiedModels = []access.UnifiedGrant{}
	public := map[string]bool{}
	for _, mapping := range g.ModelMappings {
		public[mapping.Name] = true
		m := config.UnifiedModel{ID: migrationModelID(mapping.Name), Name: mapping.Name, Enabled: true, Revision: 1, Sources: sortedSources(mapping.Sources)}
		if err := m.Validate(); err != nil {
			return g, err
		}
		if reservedName(s.st, m.Name) {
			return g, config.ModelFailure(409, "model_name_conflict", "名称与现有型号冲突")
		}
		idx := -1
		for i, x := range s.st.UnifiedModels {
			if strings.EqualFold(x.Name, m.Name) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			x := s.st.UnifiedModels[idx]
			if x.Name != m.Name {
				return g, config.ModelFailure(409, "model_name_conflict", "名称大小写冲突")
			}
			allowed := sourceSet(x)
			for _, src := range m.Sources {
				if !allowed[src.ID] {
					return g, config.ModelFailure(409, "model_name_conflict", "请在全局模型管理添加新来源")
				}
			}
			m.ID = x.ID
		} else {
			if len(s.st.UnifiedModels) >= config.MaxUnifiedModels {
				return g, config.ModelFailure(400, "invalid_model_definition", "模型数量已达上限")
			}
			s.st.UnifiedModels = append(config.CloneUnifiedModels(s.st.UnifiedModels), m)
			var err error
			s.st.ModelRoutingRevision, err = bumpRevision(s.st.ModelRoutingRevision)
			if err != nil {
				return g, err
			}
		}
		scope := access.AccessScope{ModelRules: g.ModelRules}
		a := access.UnifiedGrant{ModelID: m.ID, Visible: scope.Visible(m.Name, true), Callable: scope.Callable(m.Name), SourceMode: "selected", SourceIDs: []string{}}
		for _, src := range sortedSources(mapping.Sources) {
			a.SourceIDs = append(a.SourceIDs, src.ID)
		}
		g.UnifiedModels = append(g.UnifiedModels, a)
	}
	var rules []access.ModelRule
	for _, r := range g.ModelRules {
		if !public[r.ID] {
			rules = append(rules, r)
		}
	}
	g.ModelRules = rules
	g.ModelMappings = nil
	return g, nil
}
