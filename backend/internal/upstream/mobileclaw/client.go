// Package mobileclaw implements the China Mobile desktop model gateway.
package mobileclaw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const Origin = "https://zhenze-huhehaote.cmecloud.cn"
const ServiceVersion = "2.6.1"

type Credentials struct {
	RefreshToken  string       `json:"refreshToken,omitempty"`
	RefreshDevice *loginDevice `json:"refreshDevice,omitempty"`
	Token         string       `json:"token"`
	UserID        string       `json:"userId"`
	Name          string       `json:"userName,omitempty"`
	DeviceID      string       `json:"deviceId"`
	AIProvider    string       `json:"aiProvider,omitempty"`
	WorkspaceMode string       `json:"workspaceMode,omitempty"`
}
type Client struct {
	HTTP    *http.Client
	Origin  string
	loginMu sync.Mutex
	logins  map[string]*loginPending
}

func New() *Client { return &Client{HTTP: providerhttp.New(), Origin: Origin} }
func (*Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "mobileclaw", Name: "MobileClaw · 移动云", Available: true, Login: true, Scopes: []string{"cn"}}
}
func invalid(s string) error { return providerhttp.Invalid("MobileClaw", s) }
func (*Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var c Credentials
	if json.Unmarshal(raw, &c) != nil {
		return nil, provider.Identity{}, invalid("请导入 MobileClaw 客户端凭据 JSON")
	}
	if c.WorkspaceMode != "" && c.WorkspaceMode != "personal" {
		return nil, provider.Identity{}, invalid("当前只支持个人账号，企业工作空间尚未接入")
	}
	parts := strings.Split(c.Token, ".")
	var claims struct {
		Exp        int64  `json:"exp"`
		UserID     string `json:"uid"`
		AIProvider string `json:"aip"`
	}
	if len(parts) != 3 {
		return nil, provider.Identity{}, invalid("需要解密后的登录令牌，请使用 MobileClaw 导出助手")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return nil, provider.Identity{}, invalid("登录令牌格式无效")
	}
	if claims.UserID == "" || claims.UserID != c.UserID {
		return nil, provider.Identity{}, invalid("账号标识与登录令牌不一致")
	}
	if c.AIProvider != "" && c.AIProvider != claims.AIProvider {
		return nil, provider.Identity{}, invalid("套餐提供方与登录令牌不一致")
	}
	c.AIProvider = claims.AIProvider
	for _, v := range []string{c.UserID, c.DeviceID} {
		if v == "" || len(v) > 256 || strings.ContainsAny(v, "\r\n") {
			return nil, provider.Identity{}, invalid("缺少有效的账号或设备标识")
		}
	}
	if strings.ContainsAny(c.Token+c.AIProvider, "\r\n") || len(c.Token) > 16384 || len(c.AIProvider) > 128 {
		return nil, provider.Identity{}, invalid("凭据字段无效")
	}
	if c.RefreshToken != "" {
		if len(c.RefreshToken) > 16384 || strings.ContainsAny(c.RefreshToken, "\r\n") || c.RefreshDevice == nil || c.RefreshDevice.DeviceID != c.DeviceID || c.RefreshDevice.MAC == "" {
			return nil, provider.Identity{}, invalid("续期缺少独立登录设备信息，请重新扫码登录")
		}
		for _, value := range []string{c.RefreshDevice.Hostname, c.RefreshDevice.MAC, c.RefreshDevice.ClientVersion, c.RefreshDevice.OS} {
			if len(value) > 256 || strings.ContainsAny(value, "\r\n") {
				return nil, provider.Identity{}, invalid("续期设备信息无效")
			}
		}
		c.RefreshDevice.QRID = ""
		c.RefreshDevice.UserID = c.UserID
	} else {
		c.RefreshDevice = nil
	}
	b, _ := json.Marshal(c)
	name := c.Name
	if name == "" {
		name = "MobileClaw"
	}
	return b, provider.Identity{ID: c.UserID, Name: name, Scope: "cn", ExpiresAt: claims.Exp, HasRefresh: c.RefreshToken != ""}, nil
}
func (c *Client) request(ctx context.Context, raw json.RawMessage, path string, body any) (*http.Response, error) {
	var a Credentials
	if json.Unmarshal(raw, &a) != nil {
		return nil, invalid("凭据格式无效")
	}
	h := http.Header{"Authorization": {"Bearer " + a.Token}, "Service-Version": {ServiceVersion}, "Device-Id": {a.DeviceID}}
	method := "GET"
	if body != nil {
		method = "POST"
	}
	return providerhttp.Do(ctx, c.HTTP, "MobileClaw", method, c.Origin+path, h, body)
}
func (c *Client) read(ctx context.Context, raw json.RawMessage, path string, out any) error {
	resp, err := c.request(ctx, raw, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("MobileClaw", resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 4<<20 || json.Unmarshal(b, out) != nil {
		return providerhttp.Broken("MobileClaw")
	}
	return nil
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	_, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	var subjects []struct {
		UserID string `json:"userId"`
		Name   string `json:"username"`
	}
	if err = c.read(ctx, raw, "/desktop/manager/user-subjects", &subjects); err != nil {
		return provider.Identity{}, err
	}
	for _, s := range subjects {
		if s.UserID == id.ID {
			if s.Name != "" {
				id.Name = s.Name
			}
			return id, nil
		}
	}
	return provider.Identity{}, invalid("账号身份与官方登录不一致")
}

type modelEntry struct {
	ID          string   `json:"modelName"`
	Name        string   `json:"aliasName"`
	Description string   `json:"introduction"`
	Packages    []string `json:"pkgTypes"`
}

func (c *Client) catalog(ctx context.Context, raw json.RawMessage) ([]modelEntry, []string, error) {
	var entries []modelEntry
	if err := c.read(ctx, raw, "/desktop/gate/model/modelList", &entries); err != nil {
		return nil, nil, err
	}
	var subscriptions []struct {
		Provider string `json:"provider"`
		Packages []struct {
			Type      string `json:"providerType"`
			Available bool   `json:"available"`
		} `json:"packageList"`
	}
	if err := c.read(ctx, raw, "/desktop/manager/subscriptions/getAiProviders", &subscriptions); err != nil {
		return nil, nil, err
	}
	var a Credentials
	_ = json.Unmarshal(raw, &a)
	allowed := map[string]bool{}
	for _, s := range subscriptions {
		if a.AIProvider != "" && s.Provider != a.AIProvider {
			continue
		}
		for _, p := range s.Packages {
			if p.Available && p.Type == "FREE" {
				allowed[p.Type] = true
			}
		}
	}
	var out []modelEntry
	var packages []string
	for p := range allowed {
		packages = append(packages, p)
	}
	for _, m := range entries {
		for _, p := range m.Packages {
			if allowed[p] && providerhttp.ModelID(m.ID) {
				out = append(out, m)
				break
			}
		}
	}
	return out, packages, nil
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	entries, _, err := c.catalog(ctx, raw)
	if err != nil {
		return nil, err
	}
	out := make([]provider.Model, 0, len(entries))
	for _, m := range entries {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, provider.Model{ID: m.ID, Name: name, Desc: m.Description, Source: "upstream", StreamOnly: true})
	}
	return out, nil
}
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var value struct {
		Remaining *float64 `json:"remainTokens"`
	}
	if err := c.read(ctx, raw, "/desktop/manager/credits/v2/exp-credit", &value); err != nil {
		return nil, err
	}
	if value.Remaining == nil || *value.Remaining < 0 {
		return json.Marshal(map[string]any{"kind": "credits", "available": false, "reason": "上游未返回免费 Token 余额"})
	}
	// totalTokens is not a confirmed allocation total; do not manufacture used/total.
	return json.Marshal(map[string]any{"kind": "credits", "available": true, "package_name": "免费 Token 额度", "updated_at": time.Now().Unix(), "windows": []any{map[string]any{"id": "free", "name": "免费 Token 额度", "unit": "tokens", "remaining": *value.Remaining}}})
}
