package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// User is the durable consumer identity. Upstream accounts and groups remain
// administrator-owned; a group is an entitlement, never a tenant identity.
type User struct {
	ID             string      `json:"id"`
	Username       string      `json:"username"`
	DisplayName    string      `json:"display_name,omitempty"`
	Avatar         string      `json:"avatar,omitempty"`
	MaxConcurrency int         `json:"max_concurrency"`
	PasswordHash   string      `json:"password_hash"`
	GroupID        string      `json:"group_id"`
	Enabled        bool        `json:"enabled"`
	Revision       uint64      `json:"revision"`
	CreatedAt      int64       `json:"created_at"`
	Budget         KeyBudget   `json:"budget"`
	BudgetUsage    BudgetUsage `json:"budget_usage"`
}

type UserView struct {
	Revision       uint64      `json:"-"`
	ID             string      `json:"id"`
	Username       string      `json:"username"`
	DisplayName    string      `json:"display_name"`
	Avatar         string      `json:"avatar"`
	MaxConcurrency int         `json:"max_concurrency"`
	InFlight       int         `json:"in_flight"`
	GroupID        string      `json:"group_id"`
	Enabled        bool        `json:"enabled"`
	CreatedAt      int64       `json:"created_at"`
	Budget         KeyBudget   `json:"budget"`
	BudgetUsage    BudgetUsage `json:"budget_usage"`
}

func (u User) View() UserView {
	return UserView{ID: u.ID, Revision: u.Revision, Username: u.Username, DisplayName: u.DisplayName, Avatar: u.Avatar, MaxConcurrency: u.MaxConcurrency, GroupID: u.GroupID, Enabled: u.Enabled,
		CreatedAt: u.CreatedAt, Budget: u.Budget, BudgetUsage: u.BudgetUsage}
}

type UserUpdate struct {
	ID             string     `json:"id"`
	Username       string     `json:"username"`
	Password       string     `json:"password,omitempty"`
	GroupID        string     `json:"group_id"`
	Enabled        *bool      `json:"enabled,omitempty"`
	MaxConcurrency *int       `json:"max_concurrency,omitempty"`
	Budget         *KeyBudget `json:"budget,omitempty"`
	ResetBudget    bool       `json:"reset_budget,omitempty"`
}

func NormalizeUsername(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if len(name) < 3 || len(name) > 64 || name == "admin" {
		return "", fmt.Errorf("用户名须为 3–64 位字母、数字、点、横线或下划线，不能为 admin")
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			return "", fmt.Errorf("用户名含不支持的字符")
		}
	}
	return name, nil
}
func ValidateUserPassword(password string) error {
	if len(password) < 12 || len(password) > 256 {
		return fmt.Errorf("用户密码须为 12–256 字节")
	}
	return nil
}

// UserStore owns strict persistence and current authorization. Password work is
// deliberately outside storage's transaction lock and outside inference.
type UserStore interface {
	User(string) (User, bool)
	UserByName(string) (User, bool)
	SaveUser(UserUpdate, string) (UserView, error)
	ChangeUserPassword(string, uint64, string) error
}

var ErrUserAuthentication = errors.New("用户名或密码不正确，或用户已停用")
var ErrUserLoginBusy = errors.New("登录请求过多，请稍后再试")

type userSession struct {
	id       string
	revision uint64
	expires  time.Time
}

// Users owns bounded, opaque, in-memory sessions. Every authenticated request
// rereads the user's enabled/revision state. Sessions never authorize admin APIs.
type Users struct {
	store         UserStore
	mu            sync.Mutex
	sessions      map[string]userSession
	passwordSlots chan struct{}
}

func NewUsers(store UserStore) *Users {
	return &Users{store: store, sessions: map[string]userSession{}, passwordSlots: make(chan struct{}, 2)}
}
func (s *Users) passwordWork(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
		return fn()
	default:
		return ErrUserLoginBusy
	}
}
func (s *Users) Save(ctx context.Context, update UserUpdate) (out UserView, err error) {
	if update.Username, err = NormalizeUsername(update.Username); err != nil {
		return
	}
	if update.ID == "" || update.Password != "" {
		if err = ValidateUserPassword(update.Password); err != nil {
			return
		}
	}
	hash := ""
	if update.Password != "" {
		err = s.passwordWork(ctx, func() error { var e error; hash, e = HashAdminPassword(update.Password); return e })
		if err != nil {
			return
		}
	}
	update.Password = ""
	return s.store.SaveUser(update, hash)
}
func (s *Users) Login(ctx context.Context, name, password, token string) (out UserView, expires time.Time, err error) {
	name, _ = NormalizeUsername(name)
	u, found := s.store.UserByName(name)
	// A valid dummy representation equalizes the expensive unknown-user path.
	hash := u.PasswordHash
	if !found {
		hash = PasswordHashPrefix + strings.Repeat("0", 32) + "$" + strings.Repeat("0", 64)
	}
	err = s.passwordWork(ctx, func() error {
		if !VerifyAdminPassword(hash, password) {
			return ErrUserAuthentication
		}
		return nil
	})
	if err != nil {
		return
	}
	current, ok := s.store.User(u.ID)
	if !found || !ok || !current.Enabled || current.Revision != u.Revision {
		err = ErrUserAuthentication
		return
	}
	now := time.Now()
	expires = now.Add(8 * time.Hour)
	s.mu.Lock()
	for digest, v := range s.sessions {
		if !v.expires.After(now) {
			delete(s.sessions, digest)
		}
	}
	// At most 16 sessions per identity and 2048 overall, evicting oldest first.
	count := 0
	oldest := ""
	var at time.Time
	for digest, v := range s.sessions {
		if v.id == u.ID {
			count++
			if oldest == "" || v.expires.Before(at) {
				oldest, at = digest, v.expires
			}
		}
	}
	if count >= 16 {
		delete(s.sessions, oldest)
	}
	if len(s.sessions) >= 2048 {
		oldest = ""
		for digest, v := range s.sessions {
			if oldest == "" || v.expires.Before(at) {
				oldest, at = digest, v.expires
			}
		}
		delete(s.sessions, oldest)
	}
	s.sessions[sessionDigest(token)] = userSession{u.ID, u.Revision, expires}
	s.mu.Unlock()
	out = current.View()
	return
}
func (s *Users) Authenticate(token string) (UserView, bool) {
	s.mu.Lock()
	session, ok := s.sessions[sessionDigest(token)]
	s.mu.Unlock()
	if !ok || !time.Now().Before(session.expires) {
		return UserView{}, false
	}
	u, ok := s.store.User(session.id)
	return u.View(), ok && u.Enabled && u.Revision == session.revision
}
func (s *Users) Logout(token string) {
	s.mu.Lock()
	delete(s.sessions, sessionDigest(token))
	s.mu.Unlock()
}
func (s *Users) ChangePassword(ctx context.Context, id, old, password string) error {
	if err := ValidateUserPassword(password); err != nil {
		return err
	}
	u, ok := s.store.User(id)
	if !ok || !u.Enabled {
		return ErrUserAuthentication
	}
	return s.passwordWork(ctx, func() error {
		if !VerifyAdminPassword(u.PasswordHash, old) {
			return ErrUserAuthentication
		}
		hash, err := HashAdminPassword(password)
		if err != nil {
			return err
		}
		return s.store.ChangeUserPassword(id, u.Revision, hash)
	})
}
