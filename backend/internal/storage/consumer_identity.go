package storage

import (
	"codearts2api/internal/access"
	"fmt"
	"strings"
)

func (s *Store) IdentitySettings() access.IdentitySettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.st.Consumer.Identity
}
func (s *Store) SaveIdentitySettings(cfg access.IdentitySettings, clearSMTP, clearGitHub, clearLinuxDO bool) error {
	return s.consumerTransaction(func() error {
		if cfg.SMTPFrom != "" {
			normalized, err := access.NormalizeEmail(cfg.SMTPFrom)
			if err != nil {
				return err
			}
			cfg.SMTPFrom = normalized
		}
		old := s.st.Consumer.Identity
		if cfg.SMTPPassword == "" && !clearSMTP {
			cfg.SMTPPassword = old.SMTPPassword
		}
		if cfg.GitHub.ClientSecret == "" && !clearGitHub {
			cfg.GitHub.ClientSecret = old.GitHub.ClientSecret
		}
		if cfg.LinuxDO.ClientSecret == "" && !clearLinuxDO {
			cfg.LinuxDO.ClientSecret = old.LinuxDO.ClientSecret
		}
		if err := access.ValidateIdentitySettings(cfg); err != nil {
			return err
		}
		s.st.Consumer.Identity = cfg
		return nil
	})
}
func (s *Store) UserByEmail(email string) (access.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.st.Users {
		if u.EmailVerified && u.Email == email {
			return u, true
		}
	}
	return access.User{}, false
}
func (s *Store) VerifyUserEmail(id string, revision uint64, email string) error {
	normalized, err := access.NormalizeEmail(email)
	if err != nil {
		return err
	}
	email = normalized
	return s.consumerTransaction(func() error {
		u := s.userLocked(id)
		if u == nil || !u.Enabled || u.Revision != revision {
			return access.ErrUserAuthentication
		}
		for _, other := range s.st.Users {
			if other.ID != id && other.EmailVerified && other.Email == email {
				return fmt.Errorf("邮箱已绑定其他用户")
			}
		}
		u.Email = email
		u.EmailVerified = true
		return nil
	})
}
func (s *Store) UserIdentities(id string) []access.ExternalIdentity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []access.ExternalIdentity{}
	for _, v := range s.st.Consumer.Identities {
		if v.UserID == id {
			out = append(out, v)
		}
	}
	return out
}
func (s *Store) ExternalUser(provider, subject string) (access.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.st.Consumer.Identities {
		if v.Provider == provider && v.Subject == subject {
			u := s.userLocked(v.UserID)
			if u != nil {
				return *u, true
			}
		}
	}
	return access.User{}, false
}
func (s *Store) BindIdentity(id string, revision uint64, provider, subject string) error {
	if (provider != "github" && provider != "linuxdo") || strings.TrimSpace(subject) == "" || len(subject) > 200 {
		return fmt.Errorf("第三方账号无效")
	}
	return s.consumerTransaction(func() error {
		u := s.userLocked(id)
		if u == nil || !u.Enabled || u.Revision != revision {
			return access.ErrUserAuthentication
		}
		for _, v := range s.st.Consumer.Identities {
			if v.Provider == provider && (v.UserID == id || v.Subject == subject) {
				return fmt.Errorf("此平台账号或用户已经绑定，请先解绑")
			}
		}
		s.st.Consumer.Identities = append(s.st.Consumer.Identities, access.ExternalIdentity{UserID: id, Provider: provider, Subject: subject})
		return nil
	})
}
func (s *Store) UnbindIdentity(id string, revision uint64, provider string) error {
	return s.consumerTransaction(func() error {
		u := s.userLocked(id)
		if u == nil || !u.Enabled || u.Revision != revision {
			return access.ErrUserAuthentication
		}
		for i, v := range s.st.Consumer.Identities {
			if v.UserID == id && v.Provider == provider {
				s.st.Consumer.Identities = append(s.st.Consumer.Identities[:i], s.st.Consumer.Identities[i+1:]...)
				u.Revision++
				return nil
			}
		}
		return fmt.Errorf("账号尚未绑定")
	})
}
