package storage

import (
	"codearts2api/internal/access"
	"fmt"
)

// Migration defaults are intentionally optional. Configured entitlements must
// remain valid; malformed persisted limits must never become unlimited access.
func (s *Store) validateConsumers() error {
	if err := access.ValidateIdentitySettings(s.st.Consumer.Identity); err != nil {
		return err
	}
	for _, u := range s.st.Users {
		if u.Wallet.Balance > access.MaxCredits {
			return fmt.Errorf("用户额度超出允许范围")
		}
		if u.EmailVerified {
			normalized, err := access.NormalizeEmail(u.Email)
			if err != nil || normalized != u.Email {
				return fmt.Errorf("用户已验证邮箱无效")
			}
		}
		if u.Subscription.ID != "" {
			if err := u.Subscription.Plan.Validate(); err != nil {
				return err
			}
			if u.Subscription.ExpiresAt <= 0 || u.Subscription.Plan.GroupID != u.GroupID {
				return fmt.Errorf("用户订阅的有效期或分组无效")
			}
		}
	}
	for _, p := range s.st.Consumer.Plans {
		if p.ID == "" {
			return fmt.Errorf("套餐缺少标识")
		}
		if err := p.Validate(); err != nil {
			return err
		}
	}
	if len(s.st.Consumer.Redemptions) > 10000 {
		return fmt.Errorf("兑换码超过存储上限")
	}
	for _, r := range s.st.Consumer.Redemptions {
		if r.ID == "" || len(r.Digest) != 64 {
			return fmt.Errorf("兑换码记录无效")
		}
	}
	seen := map[string]bool{}
	for _, v := range s.st.Consumer.Identities {
		if v.Provider != "github" && v.Provider != "linuxdo" || v.Subject == "" || s.userLocked(v.UserID) == nil || seen[v.Provider+":"+v.Subject] || seen[v.Provider+":user:"+v.UserID] {
			return fmt.Errorf("第三方绑定记录无效或重复")
		}
		seen[v.Provider+":"+v.Subject] = true
		seen[v.Provider+":user:"+v.UserID] = true
	}
	return nil
}
