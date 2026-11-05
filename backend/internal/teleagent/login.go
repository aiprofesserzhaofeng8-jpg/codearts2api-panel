package teleagent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"sync"
	"time"

	"codearts2api/internal/provider"
)

// TeleAI permits its own desktop callback. A local helper observes only its
// newly opened login window; no public callback or alternate redirect is used.
const LoginURL = "https://www.teleai.com.cn/teleai-api/upms/public/auth/authorize?client_id=Z2veLLEHhZfq3hb5hWIwqJuPajIuasYi&redirect_uri=https%3A%2F%2Fagent.teleai.com.cn%2FsuperCowork%2Fsapi%2Fcowork%2Fauth"

type loginPending struct {
	mu          sync.Mutex
	created     time.Time
	digest      [32]byte
	credentials json.RawMessage
}

func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	if err := ctx.Err(); err != nil {
		return provider.Login{}, err
	}
	if scope != "cn" {
		return provider.Login{}, invalid("目前仅支持 cn 地区")
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return provider.Login{}, invalid("无法创建授权会话")
	}
	state := base64.RawURLEncoding.EncodeToString(secret[:])
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.logins == nil {
		c.logins = map[string]*loginPending{}
	}
	for id, p := range c.logins {
		if time.Since(p.created) > 10*time.Minute {
			delete(c.logins, id)
		}
	}
	if len(c.logins) >= 128 {
		return provider.Login{}, invalid("未完成授权过多，请稍后重试")
	}
	c.logins[state] = &loginPending{created: time.Now()}
	return provider.Login{State: state, URL: LoginURL, HelperChallenge: state}, nil
}

func (c *Client) ReleaseLogin(state string) {
	c.loginMu.Lock()
	delete(c.logins, state)
	c.loginMu.Unlock()
}

func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	return c.CheckCallback(ctx, scope, state, "")
}

func (c *Client) CheckCallback(ctx context.Context, scope, state, input string) (json.RawMessage, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if scope != "cn" {
		return nil, false, invalid("目前仅支持 cn 地区")
	}
	c.loginMu.Lock()
	p := c.logins[state]
	c.loginMu.Unlock()
	if p == nil {
		return nil, false, invalid("授权已取消或过期，请重新开始")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.created) > 10*time.Minute {
		return nil, false, invalid("授权已过期，请重新开始")
	}
	if input == "" {
		return nil, false, nil
	}
	var result struct {
		Version     int             `json:"version"`
		Provider    string          `json:"provider"`
		Challenge   string          `json:"challenge"`
		Credentials json.RawMessage `json:"credentials"`
	}
	if len(input) > 256<<10 {
		return nil, false, invalid("登录结果过长")
	}
	d := json.NewDecoder(bytes.NewBufferString(input))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || result.Version != 1 || result.Provider != provider.TeleAgent || subtle.ConstantTimeCompare([]byte(result.Challenge), []byte(state)) != 1 {
		return nil, false, invalid("请使用本次登录任务生成的结果文件，不能提交地址栏中的 code")
	}
	digest := sha256.Sum256([]byte(input))
	if p.credentials != nil {
		if p.digest != digest {
			return nil, false, invalid("本次授权已核对其他结果，请重新开始登录")
		}
		return append(json.RawMessage(nil), p.credentials...), true, nil
	}
	raw, who, err := c.Normalize(result.Credentials)
	if err != nil {
		return nil, false, err
	}
	if who.ExpiresAt <= time.Now().Unix() {
		return nil, false, invalid("登录凭据已过期，请重新登录")
	}
	verified, err := c.Verify(ctx, raw)
	if err != nil {
		return nil, false, err
	}
	a, _ := decode(raw)
	a.Name = verified.Name
	raw, err = json.Marshal(a)
	if err != nil {
		return nil, false, err
	}
	c.loginMu.Lock()
	active := c.logins[state] == p
	c.loginMu.Unlock()
	if !active || time.Since(p.created) > 10*time.Minute {
		return nil, false, invalid("授权已取消或过期，请重新开始")
	}
	p.credentials, p.digest = raw, digest
	return append(json.RawMessage(nil), raw...), true, nil
}

var _ provider.BrowserLogin = (*Client)(nil)
var _ provider.CallbackLogin = (*Client)(nil)
var _ provider.LoginReleaser = (*Client)(nil)
