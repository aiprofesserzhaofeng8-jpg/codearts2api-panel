package storage

import (
	"fmt"
	"strings"

	"codearts2api/internal/access"
)

const passwordPrefix = access.PasswordHashPrefix

func HashAdminPassword(password string) (string, error) { return access.HashAdminPassword(password) }
func VerifyAdminPassword(stored, password string) bool {
	return access.VerifyAdminPassword(stored, password)
}
func (s *Store) hashPasswordLocked() error {
	password := s.st.Settings.AdminPassword
	if password == "" || strings.HasPrefix(password, passwordPrefix) {
		return nil
	}
	if strings.HasPrefix(password, "pbkdf2-") {
		return fmt.Errorf("不支持的管理密码哈希格式")
	}
	hashed, err := HashAdminPassword(password)
	if err == nil {
		s.st.Settings.AdminPassword = hashed
	}
	return err
}
