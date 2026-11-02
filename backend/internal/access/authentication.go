package access

import (
	"context"
	"strings"
	"time"
)

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
	// This name was already reserved by NormalizeUsername. The built-in
	// administrator keeps its existing stored password without becoming a
	// consumer, inheriting a group, or acquiring ownership of consumer assets.
	AdminUsername  = "admin"
	BuiltinAdminID = "builtin:admin"
)

func administratorView() UserView {
	return UserView{ID: BuiltinAdminID, Username: AdminUsername, DisplayName: "管理员", Role: RoleAdmin, Enabled: true}
}

func (s *Users) currentAdminPassword() string {
	if s.adminPassword == nil {
		return ""
	}
	return s.adminPassword()
}

func (s *Users) loginAdmin(ctx context.Context, password, token string) (UserView, time.Time, error) {
	stored := s.currentAdminPassword()
	hash := stored
	if hash == "" {
		hash = PasswordHashPrefix + strings.Repeat("0", 32) + "$" + strings.Repeat("0", 64)
	}
	err := s.passwordWork(ctx, func() error {
		if !VerifyAdminPassword(hash, password) {
			return ErrUserAuthentication
		}
		return nil
	})
	if err != nil {
		return UserView{}, time.Time{}, err
	}
	if stored == "" || !sessionEqual(stored, s.currentAdminPassword()) {
		return UserView{}, time.Time{}, ErrUserAuthentication
	}
	return s.openSession(administratorView(), token, sessionDigest(stored))
}
