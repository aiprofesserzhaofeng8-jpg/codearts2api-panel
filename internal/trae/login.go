package trae

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)
import "codearts2api/internal/provider"

type loginPending struct {
	mu          sync.Mutex
	created     time.Time
	credential  *Credentials
	fingerprint [32]byte
}

func (c *Client) ReleaseLogin(state string) {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	delete(c.logins, state)
}

const callbackAddress = "http://127.0.0.1:18080/authorize"

func loginIDs(state string) (machine, device, trace string, err error) {
	parts := strings.Split(state, ".")
	if len(parts) != 2 || len(parts[0]) != 32 || len(parts[1]) != 32 {
		return "", "", "", fmt.Errorf("Trae 授权上下文无效，请重新生成")
	}
	if _, e := hex.DecodeString(parts[0] + parts[1]); e != nil {
		return "", "", "", fmt.Errorf("Trae 授权上下文无效")
	}
	sum := sha256.Sum256([]byte(state))
	return parts[0], parts[1], hex.EncodeToString(sum[:8]), nil
}
func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	if err := ctx.Err(); err != nil {
		return provider.Login{}, err
	}
	if scope != "cn" {
		return provider.Login{}, fmt.Errorf("当前仅支持 Trae 国内版")
	}
	state := randomID() + "." + randomID()
	c.loginMu.Lock()
	if c.logins == nil {
		c.logins = map[string]*loginPending{}
	}
	for key, entry := range c.logins {
		if time.Since(entry.created) > 10*time.Minute {
			delete(c.logins, key)
		}
	}
	if len(c.logins) >= 128 {
		c.loginMu.Unlock()
		return provider.Login{}, fmt.Errorf("Trae 未完成授权过多，请稍后重试")
	}
	c.logins[state] = &loginPending{created: time.Now()}
	c.loginMu.Unlock()

	machine, device, trace, _ := loginIDs(state)
	q := url.Values{"login_version": {"1"}, "auth_from": {"solo"}, "login_channel": {"native_ide"}, "plugin_version": {"2.3.62834"}, "auth_type": {"local"}, "client_id": {ClientID}, "redirect": {"0"}, "login_trace_id": {trace}, "auth_callback_url": {callbackAddress}, "machine_id": {machine}, "device_id": {device}, "x_machine_id": {machine}, "x_device_id": {device}, "x_device_brand": {"PC"}, "x_device_type": {"PC"}, "x_os_version": {"1.0"}, "x_app_version": {IDEVersion}, "x_app_type": {"stable"}}
	return provider.Login{State: state, URL: "https://www.trae.cn/authorization?" + q.Encode()}, nil
}
func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}
func parseParameter(value string) (map[string]json.RawMessage, error) {
	if value == "" {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(value), &obj) == nil && obj != nil {
		return obj, nil
	}
	decoded, e := url.QueryUnescape(value)
	if e == nil && json.Unmarshal([]byte(decoded), &obj) == nil && obj != nil {
		return obj, nil
	}
	return nil, fmt.Errorf("Trae 回调参数格式无效，请粘贴完整回调地址")
}
func (c *Client) CheckCallback(ctx context.Context, scope, state, input string) (json.RawMessage, bool, error) {
	if scope != "cn" {
		return nil, false, fmt.Errorf("当前仅支持 Trae 国内版")
	}
	if strings.TrimSpace(input) == "" {
		return nil, false, nil
	}
	if len(input) > 256<<10 {
		return nil, false, fmt.Errorf("Trae 回调过长")
	}
	machine, device, trace, err := loginIDs(state)
	if err != nil {
		return nil, false, err
	}
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:18080" || u.Path != "/authorize" || u.User != nil || u.Fragment != "" {
		return nil, false, fmt.Errorf("请粘贴本次授权跳转到 127.0.0.1:18080/authorize 的完整地址")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, false, fmt.Errorf("Trae 回调地址编码无效")
	}
	for _, values := range q {
		if len(values) != 1 {
			return nil, false, fmt.Errorf("Trae 回调参数重复，拒绝处理")
		}
	}
	actual := q.Get("loginTraceID")
	if actual == "" {
		actual = q.Get("login_trace_id")
	}
	if actual == "" || subtle.ConstantTimeCompare([]byte(actual), []byte(trace)) != 1 {
		return nil, false, fmt.Errorf("Trae 回调不属于本次授权，请重新获取链接")
	}
	if other := q.Get("login_trace_id"); other != "" && other != trace {
		return nil, false, fmt.Errorf("Trae 回调标识冲突")
	}
	for k, want := range map[string]string{"machine_id": machine, "device_id": device, "x_machine_id": machine, "x_device_id": device} {
		if got := q.Get(k); got != "" && got != want {
			return nil, false, fmt.Errorf("Trae 回调设备标识不匹配")
		}
	}
	c.loginMu.Lock()
	pending := c.logins[state]
	c.loginMu.Unlock()
	if pending == nil || time.Since(pending.created) > 10*time.Minute {
		return nil, false, fmt.Errorf("Trae 授权已过期，请重新生成链接")
	}
	pending.mu.Lock()
	defer pending.mu.Unlock()
	hash := sha256.Sum256([]byte(input))
	if pending.credential != nil && pending.fingerprint != hash {
		return nil, false, fmt.Errorf("本次 Trae 回调已提交，请使用原回调或重新生成授权")
	}
	jwt, err := parseParameter(q.Get("userJwt"))
	if err != nil {
		return nil, false, err
	}
	user, err := parseParameter(q.Get("userInfo"))
	if err != nil {
		return nil, false, err
	}
	a := Credentials{AccessToken: text(jwt, "Token"), RefreshToken: q.Get("refreshToken"), ExpiresAt: expiry(text(jwt, "TokenExpireAt")), UID: text(user, "UserID"), Nickname: text(user, "ScreenName"), Scope: "cn", ClientID: ClientID, MachineID: machine, DeviceID: device}
	if a.RefreshToken == "" {
		a.RefreshToken = text(jwt, "RefreshToken")
	}
	if a.ExpiresAt == 0 {
		a.ExpiresAt = jwtExpiry(a.AccessToken)
	}
	if pending.credential != nil {
		a = *pending.credential
	} else if a.AccessToken == "" || a.ExpiresAt <= time.Now().Unix() {
		// One exchange only. A failed exchange is returned explicitly; no polling or
		// account failover can replay this callback in the background.
		a, err = c.exchange(ctx, a)
		if err != nil {
			return nil, false, err
		}
	}
	// Retain a newly rotated token while identity lookup is retried. Never
	// exchange the original refresh token twice after a successful rotation.
	if pending.credential == nil {
		copy := a
		pending.credential = &copy
		pending.fingerprint = hash
	}
	who, err := c.user(ctx, a)
	if err != nil {
		return nil, false, err
	}
	a.UID = who.ID
	a.Nickname = who.Name
	raw, _ := json.Marshal(a)
	normalized, _, err := c.Normalize(raw)
	return normalized, err == nil, err
}

var _ provider.BrowserLogin = (*Client)(nil)
var _ provider.CallbackLogin = (*Client)(nil)
