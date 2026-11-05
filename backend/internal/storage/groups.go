package storage

import (
	"encoding/json"
	"fmt"
	"strings"

	"codearts2api/internal/access"
	"codearts2api/internal/codearts"
	"codearts2api/internal/config"
	"codearts2api/internal/provider"
)

type AccountGroup = access.AccountGroup

type AccessScope = access.AccessScope

func cloneGroup(g AccountGroup) AccountGroup {
	g.UnifiedModels = access.CloneUnifiedGrants(g.UnifiedModels)
	g.ModelMappings = cloneMappings(g.ModelMappings)
	g.ModelRules = access.CloneModelRules(g.ModelRules)
	g.AccountIDs = append([]string{}, g.AccountIDs...)
	g.ModelIDs = append([]string{}, g.ModelIDs...)
	return g
}
func (s *Store) Groups() []AccountGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AccountGroup, len(s.st.Groups))
	for i, g := range s.st.Groups {
		out[i] = s.projectGroupLocked(g)
	}
	return out
}

// CanonicalGroups returns persisted grants, never the legacy read projection.
func (s *Store) CanonicalGroups() []AccountGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AccountGroup, len(s.st.Groups))
	for i, g := range s.st.Groups {
		out[i] = cloneGroup(g)
	}
	return out
}

func (s *Store) AccessScope(key string) (AccessScope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accessScopeLocked(key)
}
func (s *Store) accessScopeLocked(key string) (AccessScope, error) {
	if key == "" {
		return AccessScope{}, nil
	}
	for _, k := range s.st.Keys {
		if k.Key != key {
			continue
		}
		if !k.Enabled {
			return AccessScope{}, fmt.Errorf("API 密钥已停用")
		}
		if err := s.keyOwnerLocked(k); err != nil {
			return AccessScope{}, err
		}
		if k.GroupID == "" {
			return AccessScope{}, nil
		}
		for _, g := range s.st.Groups {
			if g.ID != k.GroupID {
				continue
			}
			p := groupScopeLocked(s.projectGroupLocked(g))
			return p, nil
		}
		return AccessScope{}, fmt.Errorf("密钥绑定的分组不存在，请联系管理员")
	}
	return AccessScope{}, fmt.Errorf("API 密钥不存在")
}

func uniqueIDs(ids []string) ([]string, error) {
	out, seen := []string{}, map[string]bool{}
	if len(ids) > 2048 {
		return nil, fmt.Errorf("分组条目过多")
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 256 {
			return nil, fmt.Errorf("分组条目标识无效")
		}
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out, nil
}

// Group mutations hold both locks until disk commit; failed writes roll back.
func (s *Store) SaveGroup(g AccountGroup) (AccountGroup, error) {
	return s.saveUnifiedGroup(g, false)
}

func (s *Store) SaveUnifiedGroup(g AccountGroup, expected uint64) (AccountGroup, error) {
	g.ExpectedRevision = expected
	return s.saveUnifiedGroup(g, true)
}
func (s *Store) saveUnifiedGroup(g AccountGroup, strict bool) (AccountGroup, error) {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" || len([]rune(g.Name)) > 80 {
		return g, fmt.Errorf("分组名称须为 1–80 个字符")
	}
	if err := validateMappings(g.ModelMappings); err != nil {
		return g, err
	}
	var err error
	if g.AccountIDs, err = uniqueIDs(g.AccountIDs); err != nil {
		return g, err
	}
	if g.ModelIDs, err = uniqueIDs(g.ModelIDs); err != nil {
		return g, err
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	beforeModels, beforeRoute := s.st.UnifiedModels, s.st.ModelRoutingRevision
	committed := false
	defer func() {
		if !committed {
			s.st.UnifiedModels, s.st.ModelRoutingRevision = beforeModels, beforeRoute
		}
	}()
	if strict && len(g.ModelMappings) > 0 {
		return g, config.ModelFailure(400, "legacy_mapping_field", "请使用统一模型授权")
	}
	for _, id := range g.AccountIDs {
		found := false
		for _, a := range s.st.Accounts {
			if a.ID == id {
				found = true
				break
			}
		}
		if !found {
			return g, fmt.Errorf("所选账号不存在，请刷新列表")
		}
	}
	if len(g.ModelMappings) > 0 {
		if s.st.Settings.ForceModel != "" {
			return g, fmt.Errorf("请先取消强制模型，再配置分组统一模型")
		}
		allowed := map[string]bool{}
		for _, id := range g.ModelIDs {
			allowed[id] = true
		}
		for _, m := range g.ModelMappings {
			for _, id := range m.Sources {
				if g.AllowlistEnabled && !allowed[id] {
					return g, fmt.Errorf("统一模型来源未包含在分组型号白名单：%s", id)
				}
				p, scope, _ := provider.ParseModel(id)
				found := false
				for _, a := range s.st.Accounts {
					for _, aid := range g.AccountIDs {
						if a.ID == aid && a.ProviderID() == p && (p == provider.CodeArts || a.Scope == scope) {
							found = true
						}
					}
				}
				if !found {
					return g, fmt.Errorf("统一模型来源在当前分组没有对应平台账号：%s", id)
				}
			}
		}
	}
	if !strict {
		if err := validateGroupOptions(g); err != nil {
			return g, err
		}
	}
	index := -1
	if g.ID != "" {
		for i, old := range s.st.Groups {
			if old.ID == g.ID {
				index = i
				break
			}
		}
		if index < 0 {
			return g, fmt.Errorf("分组不存在")
		}
		if strict {
			if err := checkRevision(g.ExpectedRevision, s.st.Groups[index].Revision); err != nil {
				return g, err
			}
		}
		if g.UnifiedModels == nil {
			g.UnifiedModels = access.CloneUnifiedGrants(s.st.Groups[index].UnifiedModels)
		}
		if g.RawModelMode == "" {
			g.RawModelMode = s.st.Groups[index].RawModelMode
			g.RawModelsVisibleDefault = s.st.Groups[index].RawModelsVisibleDefault
		}
		var err error
		g.Revision, err = bumpRevision(s.st.Groups[index].Revision)
		if err != nil {
			return g, err
		}
	} else {
		if len(s.st.Groups) >= 128 {
			return g, fmt.Errorf("分组数量已达上限")
		}
		g.ID = codearts.RandomHex(8)
		g.Revision = 1
		if g.RawModelMode == "" {
			g.RawModelMode = "qualified_only"
			if !strict && len(g.ModelMappings) == 0 {
				g.RawModelMode = "legacy"
				g.RawModelsVisibleDefault = true
			}
		}
	}
	if !strict {
		var err error
		g, err = s.importLegacyGroupLocked(g)
		if err != nil {
			return g, err
		}
	}
	if g.UnifiedModels == nil {
		g.UnifiedModels = []access.UnifiedGrant{}
	}
	if err := validateUnifiedGroup(g, s.st.UnifiedModels); err != nil {
		return g, err
	}
	g.ExpectedRevision = 0
	for _, old := range s.st.Groups {
		if old.ID != g.ID && old.Name == g.Name {
			return g, fmt.Errorf("分组名称已存在")
		}
	}
	before := append([]AccountGroup(nil), s.st.Groups...)
	if index < 0 {
		s.st.Groups = append(s.st.Groups, cloneGroup(g))
	} else {
		s.st.Groups[index] = cloneGroup(g)
	}
	if err = s.saveGroupStateLocked(); err != nil {
		s.st.Groups = before
		if strict {
			return g, config.ModelFailure(500, "model_save_failed", "分组保存失败")
		}
		return g, err
	}
	committed = true
	if strict {
		return cloneGroup(g), nil
	}
	return s.projectGroupLocked(g), nil
}
func (s *Store) saveGroupStateLocked() error {
	if err := s.hashPasswordLocked(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err == nil {
		err = s.writeState(raw)
	}
	return err
}
func (s *Store) DeleteGroup(id string) error {
	return s.deleteGroup(id, 0, false)
}
func (s *Store) DeleteUnifiedGroup(id string, expected uint64) error {
	return s.deleteGroup(id, expected, true)
}
func (s *Store) deleteGroup(id string, expected uint64, strict bool) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.st.Users {
		if u.GroupID == id {
			return fmt.Errorf("仍有用户绑定此分组，请先修改用户分组")
		}
	}
	for _, k := range s.st.Keys {
		if k.GroupID == id {
			return fmt.Errorf("仍有 API Key 绑定此分组，请先修改密钥分组")
		}
	}
	for i, g := range s.st.Groups {
		if g.ID == id {
			if strict {
				if err := checkRevision(expected, g.Revision); err != nil {
					return err
				}
			}
			before := append([]AccountGroup(nil), s.st.Groups...)
			s.st.Groups = append(s.st.Groups[:i:i], s.st.Groups[i+1:]...)
			if err := s.saveGroupStateLocked(); err != nil {
				s.st.Groups = before
				if strict {
					return config.ModelFailure(500, "model_save_failed", "分组删除失败")
				}
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("分组不存在")
}
func (s *Store) groupExistsLocked(id string) bool {
	if id == "" {
		return true
	}
	for _, g := range s.st.Groups {
		if g.ID == id {
			return true
		}
	}
	return false
}
func (s *Store) SetKeyGroupAndSave(key, group string) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.groupExistsLocked(group) {
		return fmt.Errorf("分组不存在")
	}
	for _, k := range s.st.Keys {
		if k.Key == key {
			if k.UserID != "" {
				return fmt.Errorf("用户密钥的分组由用户权限统一管理，请修改用户分组")
			}
			before := k.GroupID
			k.GroupID = group
			if err := s.saveGroupStateLocked(); err != nil {
				k.GroupID = before
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("密钥不存在")
}
func (s *Store) AddKeyInGroupAndSave(name, group string) (*APIKey, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.groupExistsLocked(group) {
		return nil, fmt.Errorf("分组不存在")
	}
	k := &APIKey{Key: "sk-ca-" + codearts.RandomHex(24), Name: name, GroupID: group, Enabled: true, CreatedAt: s.poolNow().Unix()}
	s.st.Keys = append(s.st.Keys, k)
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.Keys = s.st.Keys[:len(s.st.Keys)-1]
		return nil, err
	}
	copy := *k
	return &copy, nil
}

// groupScopeLocked projects the same current group for keys and user consoles.
func groupScopeLocked(g AccountGroup) AccessScope {
	p := AccessScope{DefaultModel: g.DefaultModel, ClientDefaultModel: g.ClientDefaultModel, ModelRules: access.CloneModelRules(g.ModelRules), ModelMappings: cloneMappings(g.ModelMappings), Restricted: true, AllowlistEnabled: g.AllowlistEnabled, AccountIDs: map[string]bool{}, ModelIDs: map[string]bool{}}
	p.UnifiedModels = access.CloneUnifiedGrants(g.UnifiedModels)
	p.RawModelMode = g.RawModelMode
	p.RawModelsVisibleDefault = g.RawModelsVisibleDefault
	for _, id := range g.AccountIDs {
		p.AccountIDs[id] = true
	}
	for _, id := range g.ModelIDs {
		p.ModelIDs[id] = true
	}
	return p
}
