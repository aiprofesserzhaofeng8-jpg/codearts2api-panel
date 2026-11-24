package storage

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"codearts2api/internal/codearts"
	"codearts2api/internal/config"
)

type SearchService struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}
type SearchCatalog struct {
	Revision  uint64          `json:"revision"`
	DefaultID string          `json:"default_service_id"`
	Services  []SearchService `json:"services"`
}

func (c SearchCatalog) index(id string) int {
	return slices.IndexFunc(c.Services, func(v SearchService) bool { return v.ID == id })
}
func validateSearchGrants(c SearchCatalog, ids []string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if c.index(id) < 0 || seen[id] {
			return fmt.Errorf("分组搜索服务标识无效或重复")
		}
		seen[id] = true
	}
	return nil
}
func (s SearchService) validate() error {
	if strings.TrimSpace(s.Name) == "" || utf8.RuneCountInString(s.Name) > 80 {
		return fmt.Errorf("搜索服务名称须为 1–80 个字符")
	}
	switch s.Provider {
	case "deepseek":
		if s.Endpoint != "" || len(s.APIKey) > 4096 || strings.ContainsAny(s.APIKey, " \t\r\n") || (s.Enabled && s.APIKey == "") {
			return fmt.Errorf("DeepSeek 搜索凭据或地址无效")
		}
	case "searxng":
		if s.APIKey != "" {
			return fmt.Errorf("SearXNG 不接受 API 密钥")
		}
		if s.Enabled || s.Endpoint != "" {
			u, err := url.Parse(s.Endpoint)
			if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || len(s.Endpoint) > 2048 {
				return fmt.Errorf("SearXNG 搜索地址无效")
			}
		}
	default:
		return fmt.Errorf("不支持该搜索服务商")
	}
	return nil
}
func (c SearchCatalog) validate(groups []AccountGroup) error {
	seen := map[string]bool{}
	for _, v := range c.Services {
		if v.ID == "" || seen[v.ID] {
			return fmt.Errorf("搜索服务标识无效或重复")
		}
		seen[v.ID] = true
		if err := v.validate(); err != nil {
			return err
		}
	}
	if c.DefaultID != "" {
		i := c.index(c.DefaultID)
		if i < 0 || !c.Services[i].Enabled {
			return fmt.Errorf("默认搜索服务必须已启用")
		}
	}
	for _, g := range groups {
		if err := validateSearchGrants(c, g.SearchServiceIDs); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) SearchCatalog() SearchCatalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.st.ExternalSearch
	c.Services = slices.Clone(c.Services)
	return c
}

type SearchServicePatch struct {
	Name     *string `json:"name"`
	Provider *string `json:"provider"`
	Endpoint *string `json:"endpoint"`
	Enabled  *bool   `json:"enabled"`
	APIKey   *string `json:"api_key"`
	ClearKey bool    `json:"clear_api_key"`
}

func (s *Store) mutateSearch(expected uint64, change func(*SearchCatalog, []AccountGroup) error) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	old, groups := s.st.ExternalSearch, s.st.Groups
	if err := checkRevision(expected, old.Revision); err != nil {
		return err
	}
	next := old
	next.Services = slices.Clone(old.Services)
	nextGroups := slices.Clone(groups)
	if err := change(&next, nextGroups); err != nil {
		return err
	}
	if err := next.validate(nextGroups); err != nil {
		return err
	}
	var err error
	next.Revision, err = bumpRevision(old.Revision)
	if err != nil {
		return err
	}
	s.st.ExternalSearch, s.st.Groups = next, nextGroups
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.ExternalSearch, s.st.Groups = old, groups
		return config.ModelFailure(500, "search_save_failed", "搜索配置保存失败")
	}
	return nil
}
func (s *Store) SaveSearchService(id string, patch SearchServicePatch, expected uint64) (string, error) {
	err := s.mutateSearch(expected, func(c *SearchCatalog, groups []AccountGroup) error {
		i := c.index(id)
		v := SearchService{}
		if id != "" {
			if i < 0 {
				return config.ModelFailure(404, "search_service_missing", "搜索服务不存在")
			}
			v = c.Services[i]
		} else {
			if patch.Enabled != nil && *patch.Enabled {
				return fmt.Errorf("新增搜索配置须先保存为停用")
			}
			id = codearts.RandomHex(12)
			v.ID = id
		}
		if patch.Provider != nil {
			if i >= 0 && v.Provider != *patch.Provider {
				return fmt.Errorf("无法更改已有搜索配置的服务商")
			}
			v.Provider = *patch.Provider
		}
		if patch.Name != nil {
			v.Name = strings.TrimSpace(*patch.Name)
		}
		if patch.Endpoint != nil {
			v.Endpoint = strings.TrimSpace(*patch.Endpoint)
		}
		if patch.Enabled != nil {
			v.Enabled = *patch.Enabled
		}
		if patch.APIKey != nil && *patch.APIKey != "" {
			v.APIKey = *patch.APIKey
		}
		if patch.ClearKey {
			if v.Enabled || (patch.APIKey != nil && *patch.APIKey != "") {
				return fmt.Errorf("清除密钥前请先停用搜索服务")
			}
			v.APIKey = ""
		}
		if i < 0 {
			c.Services = append(c.Services, v)
		} else {
			c.Services[i] = v
		}
		if !v.Enabled && c.DefaultID == id {
			c.DefaultID = ""
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}
func (s *Store) SetDefaultSearchService(id string, expected uint64) error {
	return s.mutateSearch(expected, func(c *SearchCatalog, groups []AccountGroup) error { c.DefaultID = id; return nil })
}
func (s *Store) DeleteSearchService(id string, expected uint64) error {
	return s.mutateSearch(expected, func(c *SearchCatalog, groups []AccountGroup) error {
		i := c.index(id)
		if i < 0 {
			return config.ModelFailure(404, "search_service_missing", "搜索服务不存在")
		}
		c.Services = slices.Delete(c.Services, i, i+1)
		if c.DefaultID == id {
			c.DefaultID = ""
		}
		for i, g := range groups {
			if slices.Contains(g.SearchServiceIDs, id) {
				revision, err := bumpRevision(g.Revision)
				if err != nil {
					return err
				}
				groups[i].Revision = revision
				groups[i].SearchServiceIDs = slices.DeleteFunc(slices.Clone(g.SearchServiceIDs), func(value string) bool { return value == id })
			}
		}
		return nil
	})
}

// BeginExternalSearch selects an authorized service and reserves the shared
// request/concurrency budget under the same lock as group edits.
func (s *Store) BeginExternalSearch(key, id string) (SearchService, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	denied := config.ModelFailure(403, "search_not_allowed", "分组未获准使用此搜索服务")
	var zero SearchService
	if s.journalErr != nil {
		return zero, nil, ErrUsagePersistence
	}
	for _, k := range s.st.Keys {
		if k.Key != key {
			continue
		}
		if !k.Enabled {
			return zero, nil, config.ModelFailure(401, "invalid_key", "API 密钥已停用")
		}
		if err := s.keyOwnerLocked(k); err != nil {
			return zero, nil, denied
		}
		if id == "" {
			id = s.st.ExternalSearch.DefaultID
			if id == "" {
				return zero, nil, config.ModelFailure(503, "search_default_missing", "未配置默认搜索服务")
			}
		}
		for _, g := range s.st.Groups {
			if g.ID != k.GroupID {
				continue
			}
			if !slices.Contains(g.SearchServiceIDs, id) {
				return zero, nil, denied
			}
			i := s.st.ExternalSearch.index(id)
			if i < 0 {
				return zero, nil, denied
			}
			v := s.st.ExternalSearch.Services[i]
			if !v.Enabled {
				return zero, nil, config.ModelFailure(503, "search_disabled", "搜索服务已停用")
			}
			release, err := s.reserveBudgetLocked(key, nil, true)
			return v, release, err
		}
		return zero, nil, denied
	}
	return zero, nil, config.ModelFailure(401, "invalid_key", "API 密钥无效")
}
