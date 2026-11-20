package mobileclaw

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

type loginDevice struct {
	AIProvider    string `json:"aiProvider,omitempty"`
	DeviceID      string `json:"deviceId"`
	Hostname      string `json:"hostname"`
	MAC           string `json:"mac"`
	ClientVersion string `json:"clientVersion"`
	OS            string `json:"os"`
	QRID          string `json:"qrId,omitempty"`
	UserID        string `json:"userId,omitempty"`
}
type loginPending struct {
	mu                       sync.Mutex
	device                   loginDevice
	expires                  time.Time
	deadline                 time.Time
	nextPoll                 time.Time
	exchange                 json.RawMessage
	credentials              json.RawMessage
	canceled                 bool
	exchanged                bool
	bound                    bool
	providerBindingAttempted bool
	expectedProvider         string
}

func loginID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func deviceForLogin() (loginDevice, error) {
	id, err := loginID()
	if err != nil {
		return loginDevice{}, err
	}
	hostname, _ := os.Hostname()
	interfaces, err := net.Interfaces()
	if err != nil {
		return loginDevice{}, invalid("无法读取本机网络设备信息，请使用凭据导入")
	}
	for _, i := range interfaces {
		if i.Flags&net.FlagLoopback == 0 && i.Flags&net.FlagUp != 0 && len(i.HardwareAddr) == 6 {
			return loginDevice{DeviceID: id, Hostname: hostname, MAC: i.HardwareAddr.String(), ClientVersion: "1.9.8", OS: runtime.GOOS}, nil
		}
	}
	return loginDevice{}, invalid("当前环境没有可用网络设备信息，请使用凭据导入")
}
func (c *Client) loginPost(ctx context.Context, path string, body any, token string) (map[string]json.RawMessage, error) {
	headers := http.Header{"Service-Version": {ServiceVersion}}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "MobileClaw", "POST", c.Origin+"/desktop/manager/auth/"+path, headers, body)
	if err != nil {
		return nil, err
	}
	return providerhttp.JSON(resp, "MobileClaw")
}
func trustedLoginURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "www.cmpassport.com" && u.User == nil && u.Fragment == "" && u.Path == "/h5/js/jssdk_yw_auth/auth/scanAuth.html" && u.RawQuery != ""
}
func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if scope != "cn" {
		return provider.Login{}, invalid("仅支持国内个人账号")
	}
	device, err := deviceForLogin()
	if err != nil {
		return provider.Login{}, err
	}
	state, err := loginID()
	if err != nil {
		return provider.Login{}, err
	}
	f := &loginPending{device: device, expires: time.Now().Add(5 * time.Minute)}
	c.loginMu.Lock()
	if c.logins == nil {
		c.logins = map[string]*loginPending{}
	}
	for id, p := range c.logins {
		if time.Now().After(p.expires) {
			delete(c.logins, id)
		}
	}
	if len(c.logins) >= 128 {
		c.loginMu.Unlock()
		return provider.Login{}, invalid("未完成授权过多，请稍后重试")
	}
	c.logins[state] = f
	c.loginMu.Unlock()
	success := false
	defer func() {
		if !success {
			c.ReleaseLogin(state)
		}
	}()
	data, err := c.loginPost(ctx, "qrcode", device, "")
	if err != nil {
		return provider.Login{}, err
	}
	var response struct {
		ID  string `json:"qrId"`
		URL string `json:"qrCodeUrl"`
		PNG string `json:"qrCodeBase64"`
		TTL int    `json:"expiresInSeconds"`
	}
	raw, _ := json.Marshal(data)
	if json.Unmarshal(raw, &response) != nil || response.ID == "" || len(response.ID) > 256 || !trustedLoginURL(response.URL) || len(response.URL) > 8192 || response.TTL <= 0 || response.TTL > 300 {
		return provider.Login{}, providerhttp.Broken("MobileClaw")
	}
	png := strings.Join(strings.Fields(response.PNG), "")
	decoded, err := base64.StdEncoding.DecodeString(png)
	if err != nil || len(decoded) > 128<<10 || len(decoded) < 8 || string(decoded[:8]) != "\x89PNG\r\n\x1a\n" {
		return provider.Login{}, providerhttp.Broken("MobileClaw")
	}
	// QR expires sooner than the shared preview session. Keep expiry immutable for map cleanup.
	f.mu.Lock()
	f.device.QRID = response.ID
	f.mu.Unlock()
	expiry := time.Now().Add(time.Duration(response.TTL) * time.Second)
	f.mu.Lock()
	f.deadline = expiry
	f.mu.Unlock()
	success = true
	return provider.Login{State: state, URL: response.URL, QRCode: "data:image/png;base64," + png, ExpiresAt: expiry.Unix()}, nil
}
func (c *Client) ReleaseLogin(state string) {
	c.loginMu.Lock()
	f := c.logins[state]
	delete(c.logins, state)
	c.loginMu.Unlock()
	if f != nil {
		f.mu.Lock()
		f.canceled = true
		f.exchange = nil
		f.credentials = nil
		f.mu.Unlock()
	}
}
func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c.loginMu.Lock()
	f := c.logins[state]
	c.loginMu.Unlock()
	if f == nil || scope != "cn" {
		return nil, false, invalid("授权会话不存在，请重新生成")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.canceled {
		return nil, false, invalid("授权已取消")
	}
	if f.credentials != nil {
		return append(json.RawMessage(nil), f.credentials...), true, nil
	}
	if f.exchange == nil {
		if time.Now().After(f.deadline) {
			return nil, false, invalid("二维码已过期，请重新生成")
		}
		if time.Now().Before(f.nextPoll) {
			return nil, false, nil
		}
		f.nextPoll = time.Now().Add(2 * time.Second)
		status, err := c.loginPost(ctx, "qrstate", f.device, "")
		if err != nil {
			return nil, false, err
		}
		switch providerhttp.Field(status, "result") {
		case "0", "1":
			return nil, false, nil
		case "-1":
			return nil, false, invalid("二维码已过期，请重新生成")
		case "2":
		default:
			return nil, false, providerhttp.Broken("MobileClaw")
		}
		if f.exchanged {
			return nil, false, invalid("授权兑换结果未确认，请重新生成二维码")
		}
		f.exchanged = true
		response, err := c.loginPost(ctx, "qrlogin", f.device, "")
		if err != nil {
			return nil, false, err
		}
		f.exchange, _ = json.Marshal(response)
	}
	var exchange struct {
		Token   string `json:"accessToken"`
		Refresh string `json:"refreshToken"`
	}
	if json.Unmarshal(f.exchange, &exchange) != nil || exchange.Token == "" {
		return nil, false, providerhttp.Broken("MobileClaw")
	}
	// The first token can precede cloud-account binding. Read the authorized subjects,
	// bind only an unambiguous subject, and never select an arbitrary account.
	probe, _ := json.Marshal(Credentials{Token: exchange.Token, DeviceID: f.device.DeviceID})
	var subjects []struct {
		ID   string `json:"userId"`
		Name string `json:"username"`
	}
	if err := c.read(ctx, probe, "/desktop/manager/user-subjects", &subjects); err != nil {
		return nil, false, err
	}
	var claims struct {
		ID string `json:"uid"`
	}
	parts := strings.Split(exchange.Token, ".")
	if len(parts) == 3 {
		payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
		_ = json.Unmarshal(payload, &claims)
	}
	selected := -1
	for i, s := range subjects {
		if claims.ID != "" && s.ID == claims.ID {
			selected = i
			break
		}
	}
	if selected < 0 {
		if len(subjects) != 1 {
			return nil, false, invalid("请先在官方客户端绑定或选择移动云账号，再使用凭据导入")
		}
		if exchange.Refresh == "" {
			return nil, false, providerhttp.Broken("MobileClaw")
		}
		device := f.device
		device.QRID = ""
		device.UserID = subjects[0].ID
		if f.bound {
			return nil, false, invalid("账号绑定结果未确认，请重新生成二维码")
		}
		f.bound = true
		response, err := c.loginPost(ctx, "refresh", device, exchange.Refresh)
		if err != nil {
			return nil, false, err
		}
		f.exchange, _ = json.Marshal(response)
		exchange.Token, exchange.Refresh = "", ""
		if json.Unmarshal(f.exchange, &exchange) != nil || exchange.Token == "" {
			return nil, false, providerhttp.Broken("MobileClaw")
		}
		selected = 0
	}
	device := f.device
	device.QRID = ""
	device.UserID = subjects[selected].ID
	device.AIProvider = f.expectedProvider
	raw, _ := json.Marshal(Credentials{AIProvider: f.expectedProvider, Token: exchange.Token, RefreshToken: exchange.Refresh, RefreshDevice: &device, UserID: subjects[selected].ID, Name: subjects[selected].Name, DeviceID: f.device.DeviceID, WorkspaceMode: "personal"})
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return nil, false, err
	}
	var credential Credentials
	_ = json.Unmarshal(normalized, &credential)
	if credential.AIProvider == "" {
		if f.providerBindingAttempted {
			return nil, false, invalid("供应商授权结果未确认，请重新生成二维码")
		}
		selectedProvider, err := c.freeProvider(ctx, normalized)
		if err != nil {
			return nil, false, err
		}
		if exchange.Refresh == "" {
			return nil, false, invalid("供应商授权缺少刷新令牌，请重新扫码登录")
		}
		device.AIProvider = selectedProvider
		// Never replay a rotating POST after an uncertain result. Preserve the
		// returned generation before verification so a read failure can be retried.
		f.providerBindingAttempted = true
		f.expectedProvider = selectedProvider
		response, err := c.loginPost(ctx, "refresh", device, exchange.Refresh)
		if err != nil {
			f.canceled = true
			return nil, false, err
		}
		f.exchange, _ = json.Marshal(response)
		exchange.Token = providerhttp.Field(response, "accessToken")
		exchange.Refresh = providerhttp.Field(response, "refreshToken")
		if exchange.Token == "" || exchange.Refresh == "" {
			f.canceled = true
			return nil, false, providerhttp.Broken("MobileClaw")
		}
		credential.Token, credential.RefreshToken = exchange.Token, exchange.Refresh
		credential.AIProvider, credential.RefreshDevice = selectedProvider, &device
		raw, _ = json.Marshal(credential)
		normalized, _, err = c.Normalize(raw)
		if err != nil {
			f.canceled = true
			return nil, false, err
		}
	}
	if _, err = c.Verify(ctx, normalized); err != nil {
		return nil, false, err
	}
	f.credentials = normalized
	f.exchange = nil
	return append(json.RawMessage(nil), normalized...), true, nil
}
