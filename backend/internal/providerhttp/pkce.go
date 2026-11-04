package providerhttp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"

	"codearts2api/internal/provider"
)

type OAuthConfig struct {
	Name, Authorize, Redirect, ClientID, Scopes string
	Extra                                       url.Values
}
type Flow struct {
	mu          sync.Mutex
	verifier    string
	created     time.Time
	credentials json.RawMessage
}
type PKCE struct {
	Config  OAuthConfig
	mu      sync.Mutex
	pending map[string]*Flow
}

func random() (string, error) {
	var b [32]byte
	_, e := rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:]), e
}
func (p *PKCE) Begin(ctx context.Context, scope string) (provider.Login, error) {
	if e := ctx.Err(); e != nil {
		return provider.Login{}, e
	}
	if scope != "global" {
		return provider.Login{}, Invalid(p.Config.Name, "地区须为 global")
	}
	state, e := random()
	if e != nil {
		return provider.Login{}, Broken(p.Config.Name)
	}
	verifier, e := random()
	if e != nil {
		return provider.Login{}, Broken(p.Config.Name)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == nil {
		p.pending = map[string]*Flow{}
	}
	for k, v := range p.pending {
		if time.Since(v.created) > 10*time.Minute {
			delete(p.pending, k)
		}
	}
	if len(p.pending) >= 128 {
		return provider.Login{}, Invalid(p.Config.Name, "未完成的授权过多")
	}
	p.pending[state] = &Flow{verifier: verifier, created: time.Now()}
	digest := sha256.Sum256([]byte(verifier))
	q := url.Values{}
	for k, v := range p.Config.Extra {
		q[k] = append([]string(nil), v...)
	}
	q.Set("client_id", p.Config.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", p.Config.Redirect)
	q.Set("scope", p.Config.Scopes)
	q.Set("state", state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
	q.Set("code_challenge_method", "S256")
	return provider.Login{State: state, URL: p.Config.Authorize + "?" + q.Encode()}, nil
}
func (p *PKCE) Release(state string) { p.mu.Lock(); delete(p.pending, state); p.mu.Unlock() }
func (p *PKCE) Check(ctx context.Context, scope, state, input string, exchange func(context.Context, string, string) (json.RawMessage, error)) (json.RawMessage, bool, error) {
	p.mu.Lock()
	f := p.pending[state]
	p.mu.Unlock()
	if f == nil || scope != "global" {
		return nil, false, Invalid(p.Config.Name, "授权会话不存在或已取消")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Since(f.created) > 10*time.Minute {
		p.Release(state)
		return nil, false, Invalid(p.Config.Name, "授权已过期")
	}
	if len(f.credentials) > 0 {
		return f.credentials, true, nil
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, false, nil
	}
	var code, gotState string
	if u, e := url.Parse(input); e == nil && u.IsAbs() {
		base, _ := url.Parse(p.Config.Redirect)
		if u.User != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.Path != base.Path || u.Fragment != "" {
			return nil, false, Invalid(p.Config.Name, "回调地址不匹配")
		}
		if len(u.Query()["code"]) != 1 || len(u.Query()["state"]) != 1 || u.Query().Get("error") != "" {
			return nil, false, Invalid(p.Config.Name, "授权被拒绝或回调参数无效")
		}
		code, gotState = u.Query().Get("code"), u.Query().Get("state")
	} else {
		code, gotState, _ = strings.Cut(input, "#")
	}
	if code == "" || gotState != state || !Safe(code) {
		return nil, false, Invalid(p.Config.Name, "请粘贴本次授权的完整回调地址或 code#state")
	}
	if e := ctx.Err(); e != nil {
		return nil, false, e
	}
	raw, e := exchange(ctx, code, f.verifier)
	if e != nil {
		return nil, false, e
	}
	// Cache one-time redemption before any optional validation or save operation.
	f.credentials = append(json.RawMessage(nil), raw...)
	return f.credentials, true, nil
}
