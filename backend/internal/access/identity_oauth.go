package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"fmt"

	"net/url"

	"time"
)

type oauthProvider struct{ Authorize, Token, User, Scope string }

func OAuthProviderFor(name string) (oauthProvider, bool) {
	switch name {
	case "github":
		return oauthProvider{"https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token", "https://api.github.com/user", "read:user"}, true
	case "linuxdo":
		return oauthProvider{"https://connect.linux.do/oauth2/authorize", "https://connect.linux.do/oauth2/token", "https://connect.linux.do/api/user", "user"}, true
	}
	return oauthProvider{}, false
}
func oauthConfig(c IdentitySettings, p string) OAuthSettings {
	if p == "github" {
		return c.GitHub
	}
	return c.LinuxDO
}
func (s *Identity) StartOAuth(provider, id, browser string) (string, error) {
	p, ok := OAuthProviderFor(provider)
	cfg := s.store.IdentitySettings()
	c := oauthConfig(cfg, provider)
	if !ok || !c.Enabled || cfg.PublicOrigin == "" {
		return "", fmt.Errorf("第三方登录尚未启用")
	}
	state, verifier := rand.Text()+rand.Text(), rand.Text()+rand.Text()
	now := time.Now()
	pending := oauthState{Provider: provider, Verifier: verifier, BrowserDigest: proofDigest(browser), Expires: now.Add(10 * time.Minute), Origin: cfg.PublicOrigin}
	if id != "" {
		u, found := s.store.User(id)
		if !found || !u.Enabled {
			return "", ErrUserAuthentication
		}
		pending.UserID = id
		pending.Revision = u.Revision
	}
	s.mu.Lock()
	for k, v := range s.states {
		if !now.Before(v.Expires) {
			delete(s.states, k)
		}
	}
	if len(s.states) >= 2048 {
		s.mu.Unlock()
		return "", ErrUserLoginBusy
	}
	s.states[proofDigest(state)] = pending
	s.mu.Unlock()
	hash := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {c.ClientID}, "redirect_uri": {cfg.PublicOrigin + "/auth/oauth/callback"}, "response_type": {"code"}, "scope": {p.Scope}, "state": {state}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}}
	return p.Authorize + "?" + query.Encode(), nil
}

// A transient access token is used only for userinfo and never persisted or sent
// to the browser. Stable provider subject IDs determine ownership, never email.
func (s *Identity) CompleteOAuth(ctx context.Context, state, code, browser string) (User, bool, error) {
	key := proofDigest(state)
	s.mu.Lock()
	pending, ok := s.states[key]
	if ok && pending.BrowserDigest == proofDigest(browser) {
		delete(s.states, key)
	} else {
		ok = false
	}
	s.mu.Unlock()
	if !ok || !time.Now().Before(pending.Expires) || code == "" || len(code) > 4096 {
		return User{}, false, fmt.Errorf("登录验证已过期或浏览器不匹配，请重试")
	}
	cfg := s.store.IdentitySettings()
	c := oauthConfig(cfg, pending.Provider)

	if !c.Enabled || cfg.PublicOrigin != pending.Origin {
		return User{}, false, fmt.Errorf("登录配置已变更，请重试")
	}
	subject, err := s.transport.Exchange(ctx, cfg, pending.Provider, code, pending.Verifier, pending.Origin)
	if err != nil {
		return User{}, false, err
	}
	if pending.UserID != "" {
		if err = s.store.BindIdentity(pending.UserID, pending.Revision, pending.Provider, subject); err != nil {
			return User{}, false, err
		}
		u, found := s.store.User(pending.UserID)
		if !found {
			return User{}, false, ErrUserAuthentication
		}
		return u, true, nil
	}
	u, found := s.store.ExternalUser(pending.Provider, subject)
	if !found || !u.Enabled {
		return User{}, false, fmt.Errorf("此第三方账号尚未绑定，请先用密码登录后绑定")
	}
	return u, false, nil
}
