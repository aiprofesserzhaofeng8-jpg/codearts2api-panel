package access

import "fmt"

type unifiedGroupStore interface {
	CanonicalGroups() []AccountGroup
	SaveUnifiedGroup(AccountGroup, uint64) (AccountGroup, error)
	DeleteUnifiedGroup(string, uint64) error
}

func (m *Management) CanonicalGroups() []AccountGroup {
	if st, ok := m.store.(unifiedGroupStore); ok {
		return st.CanonicalGroups()
	}
	return m.Groups()
}

func (m *Management) SaveUnifiedGroup(g AccountGroup, expected uint64) (AccountGroup, error) {
	st, ok := m.store.(unifiedGroupStore)
	if !ok {
		return g, fmt.Errorf("统一分组事务不可用")
	}
	var old AccountGroup
	for _, previous := range st.CanonicalGroups() {
		if previous.ID == g.ID {
			old = previous
		}
	}
	previous := map[string]bool{}
	for _, rule := range old.ModelRules {
		previous[rule.ID] = true
	}
	for _, id := range old.ModelIDs {
		previous[id] = true
	}
	if g.ModelRules == nil {
		g.ModelRules = CloneModelRules(old.ModelRules)
	}
	for i, rule := range g.ModelRules {
		if id, found := m.models.Raw(rule.ID); found {
			g.ModelRules[i].ID = id
		} else if !previous[rule.ID] {
			return g, fmt.Errorf("原始模型规则不在当前目录：%s", rule.ID)
		}
	}
	for i, raw := range g.ModelIDs {
		if id, found := m.models.Raw(raw); found {
			g.ModelIDs[i] = id
		} else if !previous[raw] {
			return g, fmt.Errorf("原始白名单型号不在当前目录：%s", raw)
		}
	}
	return st.SaveUnifiedGroup(g, expected)
}

func (m *Management) DeleteUnifiedGroup(id string, expected uint64) error {
	if st, ok := m.store.(unifiedGroupStore); ok {
		return st.DeleteUnifiedGroup(id, expected)
	}
	return fmt.Errorf("统一分组事务不可用")
}
