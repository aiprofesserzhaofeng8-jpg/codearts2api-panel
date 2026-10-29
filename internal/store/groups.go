package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"codearts2api/internal/codearts"
)

type AccountGroup struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	AccountIDs       []string `json:"account_ids"`
	AllowlistEnabled bool     `json:"allowlist_enabled"`
	ModelIDs         []string `json:"model_ids"`
}

type AccessScope struct {
	Restricted       bool
	AccountIDs       map[string]bool
	AllowlistEnabled bool
	ModelIDs         map[string]bool
}

func (p AccessScope) AllowsModel(id string) bool {
	return !p.Restricted || !p.AllowlistEnabled || p.ModelIDs[id]
}

func cloneGroup(g AccountGroup) AccountGroup {
	g.AccountIDs = append([]string{}, g.AccountIDs...)
	g.ModelIDs = append([]string{}, g.ModelIDs...)
	return g
}
func (s *Store) Groups() []AccountGroup {
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
		if k.GroupID == "" {
			return AccessScope{}, nil
		}
		for _, g := range s.st.Groups {
			if g.ID != k.GroupID {
				continue
			}
			p := AccessScope{Restricted: true, AllowlistEnabled: g.AllowlistEnabled, AccountIDs: map[string]bool{}, ModelIDs: map[string]bool{}}
			for _, id := range g.AccountIDs {
				p.AccountIDs[id] = true
			}
			for _, id := range g.ModelIDs {
				p.ModelIDs[id] = true
			}
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
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" || len([]rune(g.Name)) > 80 {
		return g, fmt.Errorf("分组名称须为 1–80 个字符")
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
	} else {
		if len(s.st.Groups) >= 128 {
			return g, fmt.Errorf("分组数量已达上限")
		}
		g.ID = codearts.RandomHex(8)
	}
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
		return g, err
	}
	return cloneGroup(g), nil
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
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.st.Keys {
		if k.GroupID == id {
			return fmt.Errorf("仍有 API Key 绑定此分组，请先修改密钥分组")
		}
	}
	for i, g := range s.st.Groups {
		if g.ID == id {
			before := append([]AccountGroup(nil), s.st.Groups...)
			s.st.Groups = append(s.st.Groups[:i:i], s.st.Groups[i+1:]...)
			if err := s.saveGroupStateLocked(); err != nil {
				s.st.Groups = before
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
