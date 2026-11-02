package access

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ManagementStore retains the existing transactions. Group, binding and budget
// changes must be saved atomically by the implementation, not by this service.
type ManagementStore interface {
	Groups() []AccountGroup
	Keys() []*APIKey
	SaveGroup(AccountGroup) (AccountGroup, error)
	DeleteGroup(string) error
	AddKeyInGroupAndSave(string, string) (*APIKey, error)
	DeleteKey(string) bool
	ToggleKey(string) bool
	SetKeyGroupAndSave(string, string) error
	SetKeyBudgetAndSave(string, KeyBudget, bool) error
	Save() error
}

// ModelResolver borrows the current catalog and aliases. It keeps no second
// registry and is used only when an administrator saves a group.
type ModelResolver struct {
	Raw     func(string) (string, bool)
	Aliased func(string) (string, bool)
}

type Management struct {
	store  ManagementStore
	models ModelResolver
	mask   func(string) string
	logf   func(string, ...any)
}

func NewManagement(store ManagementStore, models ModelResolver, mask func(string) string, logf func(string, ...any)) *Management {
	return &Management{store: store, models: models, mask: mask, logf: logf}
}

func (m *Management) Groups() []AccountGroup { return m.store.Groups() }

// SaveGroup preserves omitted/null mappings on an existing group; an explicit
// empty list clears them. Catalog normalization precedes the original atomic
// membership/allowlist/force-model validation and persistence in Store.
func (m *Management) SaveGroup(group AccountGroup) (AccountGroup, error) {
	var old AccountGroup
	for _, g := range m.store.Groups() {
		if group.ID != "" && g.ID == group.ID {
			old = g
		}
	}
	previous := map[string]bool{}
	for _, r := range old.ModelRules {
		previous[r.ID] = true
	}
	for _, mapping := range old.ModelMappings {
		for _, id := range mapping.Sources {
			previous[id] = true
		}
	}
	public := map[string]bool{}
	mappings := group.ModelMappings
	if mappings == nil {
		mappings = old.ModelMappings
	}
	for _, m := range mappings {
		public[m.Name] = true
	}

	if group.ID != "" && group.ModelMappings == nil {
		for _, old := range m.store.Groups() {
			if old.ID == group.ID {
				group.ModelMappings = old.ModelMappings
			}
		}
	}
	if group.ID != "" && group.ModelRules == nil {
		for _, old := range m.store.Groups() {
			if old.ID == group.ID {
				group.ModelRules = old.ModelRules
			}
		}
	}
	for i := range group.ModelRules {
		if canonical, ok := m.models.Raw(group.ModelRules[i].ID); ok {
			group.ModelRules[i].ID = canonical
		} else if !public[group.ModelRules[i].ID] && !previous[group.ModelRules[i].ID] {
			return group, fmt.Errorf("模型规则不在当前目录或分组映射中：%s", group.ModelRules[i].ID)
		}
	}
	for i := range group.ModelMappings {
		for j, id := range group.ModelMappings[i].Sources {
			canonical, ok := m.models.Raw(id)
			if !ok {
				if previous[id] {
					continue
				}
				return group, fmt.Errorf("原始型号不在当前目录：%s", id)
			}
			group.ModelMappings[i].Sources[j] = canonical
		}
	}
	for i, id := range group.ModelIDs {
		if canonical, ok := m.models.Aliased(strings.TrimSpace(id)); ok {
			group.ModelIDs[i] = canonical
		}
	}
	for _, name := range []string{group.DefaultModel, group.ClientDefaultModel} {
		if name == "" || public[name] {
			continue
		}
		if len(group.ModelMappings) > 0 {
			return group, fmt.Errorf("配置映射后默认模型须选择统一名称：%s", name)
		}
		if _, ok := m.models.Raw(name); !ok {
			return group, fmt.Errorf("默认模型不在当前目录：%s", name)
		}
	}
	return m.store.SaveGroup(group)
}

func (m *Management) DeleteGroup(id string) error { return m.store.DeleteGroup(id) }
func (m *Management) Keys() []*APIKey             { return m.store.Keys() }
func (m *Management) AddKey(name, group string) (*APIKey, error) {
	return m.store.AddKeyInGroupAndSave(name, group)
}

// ChangeKeys retains the legacy delete-then-toggle behavior, including a fresh
// reference lookup after deletion and logging Save failure without rollback.
// Empty/unknown references remain accepted by the existing HTTP contract.
func (m *Management) ChangeKeys(deleteRef, toggleRef string) {
	if deleteRef != "" {
		m.store.DeleteKey(m.ResolveKeyRef(deleteRef))
	}
	if toggleRef != "" {
		m.store.ToggleKey(m.ResolveKeyRef(toggleRef))
	}
	if err := m.store.Save(); err != nil && m.logf != nil {
		m.logf("保存状态文件失败: %v", err)
	}
}

func (m *Management) SetKeyGroup(ref, group string) error {
	return m.store.SetKeyGroupAndSave(m.ResolveKeyRef(ref), group)
}
func (m *Management) SetKeyBudget(ref string, budget KeyBudget, reset bool) error {
	return m.store.SetKeyBudgetAndSave(m.ResolveKeyRef(ref), budget, reset)
}

// RevealKey is called only after the HTTP administrator guard has succeeded.
// Matching order is unchanged, including first-match legacy masked collisions.
func (m *Management) RevealKey(ref string) (string, bool) {
	for _, key := range m.store.Keys() {
		if KeyReference(key.Key) == ref || key.Key == ref || m.mask(key.Key) == ref {
			return key.Key, true
		}
	}
	return "", false
}
func (m *Management) ResolveKeyRef(ref string) string {
	if key, ok := m.RevealKey(ref); ok {
		return key
	}
	return ref
}

func KeyReference(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "key_" + hex.EncodeToString(sum[:12])
}
