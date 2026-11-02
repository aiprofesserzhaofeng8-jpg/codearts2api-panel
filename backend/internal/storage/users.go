package storage

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"codearts2api/internal/access"
)

func (s *Store) userLocked(id string) *access.User {
	for i := range s.st.Users {
		if s.st.Users[i].ID == id {
			return &s.st.Users[i]
		}
	}
	return nil
}
func (s *Store) User(id string) (access.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.userLocked(id)
	if u == nil {
		return access.User{}, false
	}
	return *u, true
}
func (s *Store) UserByName(name string) (access.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.st.Users {
		if u.Username == name {
			return u, true
		}
	}
	return access.User{}, false
}
func (s *Store) Users() []access.UserView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]access.UserView, 0, len(s.st.Users))
	for _, u := range s.st.Users {
		out = append(out, u.View())
	}
	return out
}

// A user's entitlement is checked under the same lock as physical selection,
// budgets and credential state. Missing users/groups fail closed.
func (s *Store) keyOwnerLocked(k *APIKey) error {
	if k.UserID == "" {
		return nil
	}
	u := s.userLocked(k.UserID)
	if u == nil || !u.Enabled || u.GroupID == "" || k.GroupID != u.GroupID || !s.groupExistsLocked(u.GroupID) {
		return fmt.Errorf("密钥所属用户已停用或分组权限无效")
	}
	return nil
}
func userBudgetID(id string) string { return "user:" + id }

func (s *Store) SaveUser(update access.UserUpdate, hash string) (access.UserView, error) {
	name, err := access.NormalizeUsername(update.Username)
	if err != nil {
		return access.UserView{}, err
	}
	if update.Budget != nil {
		if err = access.ValidateBudget(*update.Budget); err != nil {
			return access.UserView{}, err
		}
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if update.GroupID == "" || !s.groupExistsLocked(update.GroupID) {
		return access.UserView{}, fmt.Errorf("请绑定一个有效账号分组")
	}
	for _, u := range s.st.Users {
		if u.ID != update.ID && u.Username == name {
			return access.UserView{}, fmt.Errorf("用户名已存在")
		}
	}
	u := s.userLocked(update.ID)
	if update.ID != "" && u == nil {
		return access.UserView{}, fmt.Errorf("用户不存在")
	}
	if (update.ResetBudget || update.Budget != nil && (u == nil || *update.Budget != u.Budget)) && s.budgetActive[userBudgetID(update.ID)] > 0 {
		return access.UserView{}, fmt.Errorf("该用户仍有在途请求，请完成后再修改预算")
	}
	if u == nil && (len(s.st.Users) >= 1024 || !strings.HasPrefix(hash, access.PasswordHashPrefix)) {
		return access.UserView{}, fmt.Errorf("用户数量已达上限或缺少有效密码")
	}
	before := append([]access.User(nil), s.st.Users...)
	keyGroups := make([]string, len(s.st.Keys))
	for i, k := range s.st.Keys {
		keyGroups[i] = k.GroupID
	}
	if u == nil {
		s.st.Users = append(s.st.Users, access.User{ID: rand.Text(), Enabled: true, CreatedAt: time.Now().Unix()})
		u = &s.st.Users[len(s.st.Users)-1]
	}
	u.Username = name
	u.GroupID = update.GroupID
	u.Revision++
	if hash != "" {
		u.PasswordHash = hash
	}
	if update.Enabled != nil {
		u.Enabled = *update.Enabled
	}
	if update.Budget != nil {
		u.Budget = *update.Budget
	}
	if update.ResetBudget {
		u.BudgetUsage = BudgetUsage{}
	}
	for _, k := range s.st.Keys {
		if k.UserID == u.ID {
			k.GroupID = u.GroupID
		}
	}
	result := u.View()
	if err = s.saveGroupStateLocked(); err != nil {
		s.st.Users = before
		for i, k := range s.st.Keys {
			k.GroupID = keyGroups[i]
		}
		return access.UserView{}, err
	}
	return result, nil
}
func (s *Store) ChangeUserPassword(id string, revision uint64, hash string) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(id)
	if u == nil || !u.Enabled || u.Revision != revision {
		return access.ErrUserAuthentication
	}
	if !strings.HasPrefix(hash, access.PasswordHashPrefix) {
		return fmt.Errorf("无效密码摘要")
	}
	before := *u
	u.PasswordHash = hash
	u.Revision++
	if err := s.saveGroupStateLocked(); err != nil {
		*u = before
		return err
	}
	return nil
}
func (s *Store) UserKeys(id string) []*APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*APIKey{}
	for _, k := range s.st.Keys {
		if k.UserID == id {
			v := *k
			out = append(out, &v)
		}
	}
	return out
}
func (s *Store) AddUserKeyAndSave(id, name string) (APIKey, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 80 {
		return APIKey{}, fmt.Errorf("密钥备注最多 80 字")
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(id)
	if u == nil || !u.Enabled || u.GroupID == "" || !s.groupExistsLocked(u.GroupID) {
		return APIKey{}, fmt.Errorf("用户或分组权限无效")
	}
	n := 0
	for _, k := range s.st.Keys {
		if k.UserID == id {
			n++
		}
	}
	if n >= 32 {
		return APIKey{}, fmt.Errorf("每个用户最多 32 个密钥")
	}
	k := &APIKey{Key: "sk-ca-" + rand.Text() + rand.Text(), Name: name, Enabled: true, CreatedAt: time.Now().Unix(), GroupID: u.GroupID, UserID: id}
	before := s.st.Keys
	s.st.Keys = append(s.st.Keys, k)
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.Keys = before
		return APIKey{}, err
	}
	return *k, nil
}
func (s *Store) ChangeUserKeyAndSave(id, ref string, remove bool) (bool, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(id)
	if u == nil || !u.Enabled {
		return false, access.ErrUserAuthentication
	}
	for i, k := range s.st.Keys {
		if k.UserID != id || access.KeyReference(k.Key) != ref {
			continue
		}
		before := append([]*APIKey(nil), s.st.Keys...)
		enabled := k.Enabled
		if remove {
			s.st.Keys = append(s.st.Keys[:i:i], s.st.Keys[i+1:]...)
		} else {
			k.Enabled = !k.Enabled
		}
		if err := s.saveGroupStateLocked(); err != nil {
			s.st.Keys = before
			k.Enabled = enabled
			return false, err
		}
		return true, nil
	}
	return false, nil
}
func (s *Store) RevealUserKey(id, ref string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.userLocked(id)
	if u == nil || !u.Enabled {
		return "", false
	}
	for _, k := range s.st.Keys {
		if k.UserID == id && access.KeyReference(k.Key) == ref {
			return k.Key, true
		}
	}
	return "", false
}

func (s *Store) UserScope(id string) (AccessScope, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.userLocked(id)
	if u == nil || !u.Enabled || u.GroupID == "" {
		return AccessScope{}, "", access.ErrUserAuthentication
	}
	for _, g := range s.st.Groups {
		if g.ID != u.GroupID {
			continue
		}
		p := groupScopeLocked(g)
		return p, g.Name, nil
	}
	return AccessScope{}, "", fmt.Errorf("用户绑定分组不存在")
}
