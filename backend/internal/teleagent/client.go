// Package teleagent implements TeleAI's account-bound model gateway.
// Protocol references and MIT notices are recorded in UPSTREAM.md.
package teleagent

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const ChatPath = "/superCowork/sapi/api/v1/chat/completions"
const UserPath = "/superCowork/sapi/cowork/user_info"
const CreditPath = "/superCowork/sapi/user/portal/companyStore/userCenter"
const PortalPath = "/meter/super-agent/user/portal/points/detail"

type Credentials struct {
	Token          string `json:"token"`
	APIKey         string `json:"api_key"`
	AccountID      string `json:"account_id"`
	Name           string `json:"name,omitempty"`
	DeviceID       string `json:"device_id"`
	InstallID      string `json:"install_id"`
	AppVersion     string `json:"app_version"`
	ExpiresAt      int64  `json:"expires_at"`
	OperationToken string `json:"operation_token,omitempty"`
	OperToken      string `json:"oper_token,omitempty"`
}
type Client struct {
	HTTP   *http.Client
	Origin string
}

func New() *Client { return &Client{HTTP: providerhttp.New(), Origin: "https://agent.teleai.com.cn"} }
func (*Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.TeleAgent, Name: "TeleAgent", Available: true, Scopes: []string{"cn"}, Login: false}
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,256}$`)
var version = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-.][A-Za-z0-9.-]+)?$`)

func invalid(text string) error { return providerhttp.Invalid("TeleAgent", text) }
func jwtParts(token string) ([]string, map[string]json.RawMessage) {
	jwt := token
	if _, rest, ok := strings.Cut(token, ":"); ok {
		jwt = rest
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || len(b) > 32<<10 {
		return nil, nil
	}
	var claims map[string]json.RawMessage
	if json.Unmarshal(b, &claims) != nil || claims == nil {
		return nil, nil
	}
	return parts, claims
}
func accountField(o map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if s := providerhttp.Field(o, k); s != "" {
			return s
		}
		var n json.Number
		if json.Unmarshal(o[k], &n) == nil && n != "" {
			return n.String()
		}
	}
	return ""
}
func (*Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var o map[string]json.RawMessage
	if json.Unmarshal(raw, &o) != nil || o == nil {
		return nil, provider.Identity{}, invalid("凭据须为 JSON 对象")
	}
	if b := o["auth"]; len(b) > 0 {
		var nested map[string]json.RawMessage
		if json.Unmarshal(b, &nested) != nil || nested == nil {
			return nil, provider.Identity{}, invalid("auth 格式无效")
		}
		o = nested
	}
	if scope := providerhttp.Field(o, "scope"); scope != "" && scope != "cn" {
		return nil, provider.Identity{}, invalid("目前仅支持 cn 地区")
	}
	if providerhttp.Field(o, "refresh_token", "refreshToken") != "" {
		return nil, provider.Identity{}, invalid("未确认可用的续期协议，请导入当前账号令牌")
	}
	a := Credentials{Token: providerhttp.Field(o, "token", "access_token", "accessToken"), APIKey: providerhttp.Field(o, "api_key", "apiKey", "gateway_key"), AccountID: accountField(o, "account_id", "accountId", "uid"), Name: providerhttp.Field(o, "name", "nickname", "accountName"), DeviceID: providerhttp.Field(o, "device_id", "deviceId"), InstallID: providerhttp.Field(o, "install_id", "installId"), AppVersion: providerhttp.Field(o, "app_version", "appVersion"), ExpiresAt: providerhttp.Timestamp(o["expires_at"]), OperationToken: providerhttp.Field(o, "operation_token", "operationToken"), OperToken: providerhttp.Field(o, "oper_token", "operToken")}
	parts, claims := jwtParts(a.Token)
	if len(parts) != 3 {
		return nil, provider.Identity{}, invalid("token 须为完整账号 JWT，可包含租户前缀")
	}
	claimedID := accountField(claims, "accountId", "account_id", "uid")
	if a.AccountID == "" {
		a.AccountID = claimedID
	} else if claimedID != "" && claimedID != a.AccountID {
		return nil, provider.Identity{}, invalid("account_id 与令牌身份不一致")
	}
	if a.Name == "" {
		a.Name = providerhttp.Field(claims, "accountName", "nickname")
	}
	if exp := providerhttp.Timestamp(claims["exp"]); exp > 0 && (a.ExpiresAt == 0 || exp < a.ExpiresAt) {
		a.ExpiresAt = exp
	}
	if a.ExpiresAt <= 0 || a.AccountID == "" {
		return nil, provider.Identity{}, invalid("缺少 account_id 或真实 expires_at / JWT exp")
	}
	if !strings.HasPrefix(a.APIKey, "sk-") || len(a.APIKey) <= 3 || len(a.APIKey) > 4096 {
		return nil, provider.Identity{}, invalid("请提供本人获准使用的网关 api_key（sk- 开头）")
	}
	if !identifier.MatchString(a.DeviceID) || !identifier.MatchString(a.InstallID) || !version.MatchString(a.AppVersion) || len(a.AppVersion) > 64 {
		return nil, provider.Identity{}, invalid("缺少有效 device_id、install_id 或 app_version，请使用原客户端信息")
	}
	if !providerhttp.Safe(a.Token, a.APIKey, a.AccountID, a.Name, a.DeviceID, a.InstallID, a.AppVersion, a.OperationToken, a.OperToken) || len(a.AccountID) > 200 || len(a.Name) > 256 || strings.ContainsAny(a.OperationToken+a.OperToken, ";\"\\") || a.OperToken != "" && a.OperationToken == "" {
		return nil, provider.Identity{}, invalid("凭据字段无效或过长")
	}
	b, e := json.Marshal(a)
	return b, identity(a), e
}
func identity(a Credentials) provider.Identity {
	name := a.Name
	if name == "" {
		name = "TeleAgent"
	}
	return provider.Identity{ID: a.AccountID, Name: name, Scope: "cn", ExpiresAt: a.ExpiresAt}
}
func decode(raw json.RawMessage) (Credentials, error) {
	var a Credentials
	e := json.Unmarshal(raw, &a)
	return a, e
}
func (*Client) Refresh(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, invalid("没有已确认的自动续期端点，请重新登录并更新凭据")
}
func identityHeaders(a Credentials) http.Header {
	h := http.Header{}
	h.Set("X-Token", a.Token)
	h.Set("X-Route-Target", "ops-gateway")
	h.Set("X-App-Version", a.AppVersion)
	h.Set("X-Superagent-Device-Id", a.DeviceID)
	h.Set("X-Superagent-Install-Id", a.InstallID)
	h.Set("Accept", "application/json")
	return h
}
func hmacHex(key, msg string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}
func signature(a Credentials, method, path, ts, nonce string, body []byte) string {
	parts, _ := jwtParts(a.Token)
	key := hmacHex(parts[2], strings.Join([]string{"superagent-auth-v1", a.Token, a.InstallID, ts, nonce}, "/"))
	sum := sha256.Sum256(body)
	return hmacHex(key, strings.Join([]string{"superagent-auth-v1", method, path, ts, nonce, a.AppVersion, hex.EncodeToString(sum[:])}, "\n"))
}
func nonce() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}
func unwrap(resp *http.Response) (map[string]json.RawMessage, error) {
	o, e := providerhttp.JSON(resp, "TeleAgent")
	if e != nil {
		return nil, e
	}
	if code := o["code"]; len(code) > 0 && string(code) != "null" && string(code) != "0" && string(code) != `"0"` && string(code) != "200" && string(code) != `"200"` {
		return nil, businessError(code)
	}
	if string(o["success"]) == "false" {
		return nil, providerhttp.Broken("TeleAgent")
	}
	if b := o["data"]; len(b) > 0 && string(b) != "null" {
		o = nil
		if json.Unmarshal(b, &o) != nil || o == nil {
			return nil, providerhttp.Broken("TeleAgent")
		}
	}
	return o, nil
}
func businessError(raw json.RawMessage) error {
	code := strings.Trim(string(raw), `"`)
	status := 502
	fault := false
	switch code {
	case "4001", "40108", "40109":
		status = 401
		fault = true
	case "40101", "40103", "40104", "40105", "40301":
		status = 403
	case "429":
		status = 429
	}
	if _, e := strconv.Atoi(code); e != nil || len(code) > 8 {
		return providerhttp.Broken("TeleAgent")
	}
	return &provider.Error{Status: status, Code: code, AccountFault: fault, Message: "TeleAgent 上游拒绝请求（业务码 " + code + "）"}
}
func (c *Client) user(ctx context.Context, a Credentials) (map[string]json.RawMessage, error) {
	resp, e := providerhttp.Do(ctx, c.HTTP, "TeleAgent", http.MethodGet, c.Origin+UserPath, identityHeaders(a), nil)
	if e != nil {
		return nil, e
	}
	return unwrap(resp)
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	a, e := decode(raw)
	if e != nil {
		return provider.Identity{}, e
	}
	o, e := c.user(ctx, a)
	if e != nil {
		return provider.Identity{}, e
	}
	if accountField(o, "accountId", "account_id", "uid") != a.AccountID {
		return provider.Identity{}, invalid("上游身份与导入账号不一致")
	}
	a.Name = providerhttp.Field(o, "accountName", "nickname", "name")
	return identity(a), nil
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	if _, e := c.Verify(ctx, raw); e != nil {
		return nil, e
	}
	out := []provider.Model{}
	for _, v := range [][2]string{{"chat-pro", "智能"}, {"chat-flagship", "旗舰"}, {"chat-flash", "极速"}, {"chat-lite", "轻量"}, {"chat-nano", "轻速"}} {
		out = append(out, provider.Model{ID: v[0], Name: v[1], Provider: provider.TeleAgent, Scope: "cn", UpstreamID: v[0], Source: "TeleAgent 预置目录；逐模型权限以上游为准", StreamOnly: true})
	}
	return out, nil
}
