package kiro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"codearts2api/internal/provider"
)

const deviceGrant = "urn:ietf:params:oauth:grant-type:device_code"

type pendingLogin struct {
	mu                                         sync.Mutex
	region, clientID, clientSecret, deviceCode string
	clientExpires                              int64
	expires, next                              time.Time
	interval                                   time.Duration
	credentials                                json.RawMessage
}

func trustedDeviceURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	h := u.Hostname()
	return h == "view.awsapps.com" || h == "device.sso.us-east-1.amazonaws.com" || h == "device.sso.eu-central-1.amazonaws.com"
}
func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	r, ok := c.Regions[scope]
	if !ok {
		return provider.Login{}, invalid("请选择 us-east-1 / eu-central-1")
	}
	c.mu.Lock()
	for k, p := range c.logins {
		if time.Now().After(p.expires) {
			delete(c.logins, k)
		}
	}
	full := len(c.logins) >= 128
	c.mu.Unlock()
	if full {
		return provider.Login{}, invalid("待授权会话过多")
	}
	scopes := []string{"codewhisperer:completions", "codewhisperer:analysis", "codewhisperer:conversations", "codewhisperer:transformations", "codewhisperer:taskassist"}
	resp, e := c.request(ctx, http.MethodPost, r.OIDC+"/client/register", Credentials{}, "", map[string]any{"clientName": "Kiro", "clientType": "public", "scopes": scopes, "grantTypes": []string{deviceGrant, "refresh_token"}})
	if e != nil {
		return provider.Login{}, e
	}
	reg, e := readJSON(resp)
	if e != nil {
		return provider.Login{}, e
	}
	p := &pendingLogin{region: scope, clientID: field(reg, "clientId"), clientSecret: field(reg, "clientSecret"), clientExpires: timestamp(reg["clientSecretExpiresAt"])}
	if p.clientID == "" || p.clientSecret == "" {
		return provider.Login{}, broken()
	}
	resp, e = c.request(ctx, http.MethodPost, r.OIDC+"/device_authorization", Credentials{}, "", map[string]string{"clientId": p.clientID, "clientSecret": p.clientSecret, "startUrl": "https://view.awsapps.com/start"})
	if e != nil {
		return provider.Login{}, e
	}
	dev, e := readJSON(resp)
	if e != nil {
		return provider.Login{}, e
	}
	var seconds, interval int64
	_ = json.Unmarshal(dev["expiresIn"], &seconds)
	_ = json.Unmarshal(dev["interval"], &interval)
	if seconds <= 0 || seconds > 3600 || interval < 0 || interval > 300 {
		return provider.Login{}, broken()
	}
	if interval == 0 {
		interval = 5
	}
	if seconds > 600 {
		seconds = 600
	}
	p.deviceCode = field(dev, "deviceCode")
	p.interval = time.Duration(interval) * time.Second
	p.expires = time.Now().Add(time.Duration(seconds) * time.Second)
	link := field(dev, "verificationUriComplete")
	if link == "" {
		link = field(dev, "verificationUri")
		code := field(dev, "userCode")
		if link != "" && code != "" {
			u, e := url.Parse(link)
			if e != nil {
				return provider.Login{}, broken()
			}
			q := u.Query()
			q.Set("user_code", code)
			u.RawQuery = q.Encode()
			link = u.String()
		}
	}
	if p.deviceCode == "" || !trustedDeviceURL(link) {
		return provider.Login{}, broken()
	}
	state, e := uuid()
	if e != nil {
		return provider.Login{}, e
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.logins) >= 128 {
		return provider.Login{}, invalid("待授权会话过多")
	}
	c.logins[state] = p
	return provider.Login{State: state, URL: link}, nil
}
func (c *Client) ReleaseLogin(state string) { c.mu.Lock(); delete(c.logins, state); c.mu.Unlock() }
func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	c.mu.Lock()
	p := c.logins[state]
	c.mu.Unlock()
	if p == nil {
		return nil, false, invalid("授权会话不存在或已取消")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if scope != p.region {
		return nil, false, invalid("授权地区不一致")
	}
	if time.Now().After(p.expires) {
		c.ReleaseLogin(state)
		return nil, false, invalid("设备授权已过期，请重新生成链接")
	}
	if len(p.credentials) > 0 {
		return p.credentials, true, nil
	}
	if time.Now().Before(p.next) {
		return nil, false, nil
	}
	p.next = time.Now().Add(p.interval)
	resp, e := c.request(ctx, http.MethodPost, c.Regions[scope].OIDC+"/token", Credentials{}, "", map[string]string{"clientId": p.clientID, "clientSecret": p.clientSecret, "deviceCode": p.deviceCode, "grantType": deviceGrant})
	if e != nil {
		return nil, false, e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status := resp.StatusCode
		resp.StatusCode = 200
		o, e := readJSON(resp)
		if e != nil {
			return nil, false, e
		}
		code := field(o, "error", "__type")
		if i := strings.LastIndex(code, "#"); i >= 0 {
			code = code[i+1:]
		}
		switch code {
		case "authorization_pending", "AuthorizationPendingException":
			return nil, false, nil
		case "slow_down", "SlowDownException":
			p.interval += 5 * time.Second
			p.next = time.Now().Add(p.interval)
			return nil, false, nil
		case "expired_token", "ExpiredTokenException", "access_denied", "AccessDeniedException":
			c.ReleaseLogin(state)
			return nil, false, invalid("设备授权已过期或被拒绝，请重新登录")
		}
		return nil, false, failure(status)
	}
	o, e := readJSON(resp)
	if e != nil {
		return nil, false, e
	}
	a := Credentials{Region: scope, AuthRegion: scope, AuthMethod: "idc", ClientID: p.clientID, ClientSecret: p.clientSecret, ClientSecretExpiresAt: p.clientExpires}
	raw, e := applyToken(a, o)
	if e != nil {
		return nil, false, e
	}
	a, e = decode(raw)
	if e != nil || a.RefreshToken == "" {
		return nil, false, broken()
	}
	// Retain the one-time device redemption before any other network operation.
	p.credentials = raw
	return raw, true, nil
}
