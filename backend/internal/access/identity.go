package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"
	"time"
)

type IdentityTransport interface {
	Send(context.Context, IdentitySettings, string, string) error
	Exchange(context.Context, IdentitySettings, string, string, string, string) (string, error)
}

type IdentityStore interface {
	User(string) (User, bool)
	UserByEmail(string) (User, bool)
	IdentitySettings() IdentitySettings
	VerifyUserEmail(string, uint64, string) error
	ResetVerifiedUserPassword(string, uint64, string, string) error
	BindIdentity(string, uint64, string, string) error
	ExternalUser(string, string) (User, bool)
}
type verification struct {
	UserID, Email, Purpose, Digest string
	Revision                       uint64
	Expires                        time.Time
	Attempts                       int
}
type oauthState struct {
	UserID, Provider, Verifier, BrowserDigest string
	Revision                                  uint64
	Expires                                   time.Time
	Origin                                    string
}

// Identity has bounded ephemeral proofs, not cached authorization. Every proof
// is single use and every mutation rechecks the current durable user revision.
type Identity struct {
	store     IdentityStore
	users     *Users
	mu        sync.Mutex
	proofs    map[string]verification
	sent      map[string]time.Time
	states    map[string]oauthState
	transport IdentityTransport
}

func NewIdentity(store IdentityStore, users *Users, transport IdentityTransport) *Identity {
	return &Identity{store: store, users: users, proofs: map[string]verification{}, sent: map[string]time.Time{}, states: map[string]oauthState{}, transport: transport}
}
func proofDigest(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func (s *Identity) StartEmail(ctx context.Context, id, email, purpose string) (string, error) {
	var err error
	email, err = NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	cfg := s.store.IdentitySettings()
	if cfg.SMTPHost == "" {
		return "", fmt.Errorf("管理员尚未配置邮件服务")
	}
	var u User
	var ok bool
	switch purpose {
	case "verify":
		u, ok = s.store.User(id)
	case "reset":
		u, ok = s.store.UserByEmail(email)
	default:
		return "", fmt.Errorf("验证用途无效")
	}
	token := rand.Text() + rand.Text()
	now := time.Now()
	rateKey := purpose + ":" + email
	s.mu.Lock()
	for k, p := range s.proofs {
		if !p.Expires.After(now) {
			delete(s.proofs, k)
		}
	}
	for k, t := range s.sent {
		if now.Sub(t) > 10*time.Minute {
			delete(s.sent, k)
		}
	}
	if now.Sub(s.sent[rateKey]) < time.Minute || len(s.proofs) >= 2048 || len(s.sent) >= 2048 {
		s.mu.Unlock()
		return "", fmt.Errorf("验证请求过多，请稍后再试")
	}
	s.sent[rateKey] = now
	s.mu.Unlock()
	// Unknown reset accounts get the same public shape and no proof or mail.
	if !ok || !u.Enabled {
		if purpose == "reset" {
			return token, nil
		}
		return "", ErrUserAuthentication
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	s.mu.Lock()
	s.proofs[proofDigest(token)] = verification{UserID: u.ID, Email: email, Purpose: purpose, Digest: proofDigest(token + ":" + code), Revision: u.Revision, Expires: now.Add(10 * time.Minute)}
	s.mu.Unlock()
	if err = s.transport.Send(ctx, cfg, email, code); err != nil {
		s.mu.Lock()
		delete(s.proofs, proofDigest(token))
		s.mu.Unlock()
		return "", fmt.Errorf("验证码发送失败，请检查邮件服务配置")
	}
	return token, nil
}
func (s *Identity) consumeProof(token, code, purpose, id string) (verification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := proofDigest(token)
	p, ok := s.proofs[key]
	valid := ok && p.Purpose == purpose && (id == "" || p.UserID == id) && time.Now().Before(p.Expires) && p.Attempts < 5
	if !valid {
		return verification{}, fmt.Errorf("验证码无效或已过期")
	}
	got := proofDigest(token + ":" + code)
	p.Attempts++
	if subtle.ConstantTimeCompare([]byte(got), []byte(p.Digest)) != 1 {
		if p.Attempts >= 5 {
			delete(s.proofs, key)
		} else {
			s.proofs[key] = p
		}
		return verification{}, fmt.Errorf("验证码无效或已过期")
	}
	delete(s.proofs, key)
	return p, nil
}
func (s *Identity) VerifyEmail(id, token, code string) error {
	p, err := s.consumeProof(token, code, "verify", id)
	if err != nil {
		return err
	}
	return s.store.VerifyUserEmail(p.UserID, p.Revision, p.Email)
}
func (s *Identity) ResetPassword(ctx context.Context, token, code, password string) error {
	if err := ValidateUserPassword(password); err != nil {
		return err
	}
	p, err := s.consumeProof(token, code, "reset", "")
	if err != nil {
		return err
	}
	u, ok := s.store.User(p.UserID)
	if !ok || !u.Enabled || u.Revision != p.Revision || !u.EmailVerified || u.Email != p.Email {
		return ErrUserAuthentication
	}
	return s.users.passwordWork(ctx, func() error {
		hash, err := HashAdminPassword(password)
		if err != nil {
			return err
		}
		return s.store.ResetVerifiedUserPassword(u.ID, u.Revision, p.Email, hash)
	})
}
