// Package trae adapts Trae CN/SOLO's native llm_utils_chat channel.
// Protocol references and MIT notices are recorded in UPSTREAM.md.
package trae

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"codearts2api/internal/provider"
)

const (
	ClientID     = "en1oxy7wnw8j9n"
	IDEClientID  = "ono9krqynydwx5"
	AppID        = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
	IDEVersion   = "0.1.52"
	ChatPath     = "/api/agent/v3/llm_utils_chat"
	ModelsPath   = "/api/ide/v1/get_detail_param"
	ExchangePath = "/cloudide/api/v3/trae/oauth/ExchangeToken"
	UserPath     = "/cloudide/api/v3/trae/GetUserInfo"
	CreditPath   = "/trae/api/v2/pay/ide_user_ent_usage"
)

type Credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at"`
	UID          string `json:"uid"`
	Nickname     string `json:"nickname,omitempty"`
	Scope        string `json:"scope"`
	ClientID     string `json:"client_id"`
	MachineID    string `json:"machine_id"`
	DeviceID     string `json:"device_id"`
}

type Client struct {
	loginMu                          sync.Mutex
	logins                           map[string]*loginPending
	HTTP                             *http.Client
	AgentBase, OAuthBase, CreditBase string
}

func New() *Client {
	return &Client{HTTP: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, AgentBase: "https://trae-api-cn.mchost.guru", OAuthBase: "https://api.trae.com.cn", CreditBase: "https://api.trae.cn"}
}
func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.Trae, Name: "Trae CN / SOLO", Available: true, Scopes: []string{"cn"}, Login: true}
}
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("random source unavailable")
	}
	return hex.EncodeToString(b[:])
}
func text(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		var s string
		if json.Unmarshal(m[k], &s) == nil && s != "" {
			return strings.TrimSpace(s)
		}
		var n json.Number
		if json.Unmarshal(m[k], &n) == nil && n != "" {
			return n.String()
		}
	}
	return ""
}
func expiry(raw string) int64 {
	n, e := strconv.ParseInt(raw, 10, 64)
	if e == nil {
		if n > 1e12 {
			n /= 1000
		}
		return n
	}
	if t, e := time.Parse(time.RFC3339Nano, raw); e == nil {
		return t.Unix()
	}
	return 0
}
func jwtExpiry(token string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return 0
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return 0
	}
	return expiry(text(obj, "exp"))
}
func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return nil, provider.Identity{}, fmt.Errorf("Trae 凭据必须为 JSON 对象")
	}
	data := root
	if b := root["auth"]; len(b) > 0 {
		if json.Unmarshal(b, &data) != nil || data == nil {
			return nil, provider.Identity{}, fmt.Errorf("Trae auth 字段无效")
		}
	}
	a := Credentials{AccessToken: text(data, "access_token", "accessToken", "token", "Token"), RefreshToken: text(data, "refresh_token", "refreshToken", "RefreshToken"), UID: text(data, "uid", "user_id", "UserID"), Nickname: text(data, "nickname", "ScreenName"), Scope: text(data, "scope", "realm", "edition"), ClientID: text(data, "client_id", "clientId", "ClientID"), MachineID: text(data, "machine_id", "machineId"), DeviceID: text(data, "device_id", "deviceId")}
	a.AccessToken = strings.TrimPrefix(a.AccessToken, "Cloud-IDE-JWT ")
	a.ExpiresAt = expiry(text(data, "expires_at", "expiresAt", "token_expire_at", "TokenExpireAt", "expired_at"))
	if a.ExpiresAt == 0 {
		a.ExpiresAt = jwtExpiry(a.AccessToken)
	}
	if nested := root["account"]; len(nested) > 0 {
		var who map[string]json.RawMessage
		if json.Unmarshal(nested, &who) != nil {
			return nil, provider.Identity{}, fmt.Errorf("Trae account 字段无效")
		}
		if a.UID == "" {
			a.UID = text(who, "uid", "user_id", "UserID")
		}
		if a.Nickname == "" {
			a.Nickname = text(who, "nickname", "ScreenName")
		}
	}
	if a.Scope == "" {
		a.Scope = text(root, "scope", "realm", "edition")
	}
	if a.Scope == "solo" {
		a.Scope = "cn"
	}
	if a.Scope == "" {
		a.Scope = "cn"
	}
	domain := strings.ToLower(text(data, "domain", "apiHost", "host"))
	if a.Scope != "cn" || strings.Contains(domain, "trae.ai") || strings.Contains(domain, "byteintlapi") {
		return nil, provider.Identity{}, fmt.Errorf("当前 Trae 适配仅支持国内版，请勿导入国际版凭据")
	}
	if a.ClientID == "" {
		a.ClientID = ClientID
	}
	if a.ClientID != ClientID && a.ClientID != IDEClientID {
		return nil, provider.Identity{}, fmt.Errorf("不支持的 Trae client_id")
	}
	if a.AccessToken == "" || len(a.AccessToken) > 32768 || a.UID == "" || len(a.UID) > 200 || a.ExpiresAt <= 0 {
		return nil, provider.Identity{}, fmt.Errorf("Trae 凭据需要 access_token、uid 和有效 expires_at（或 JWT exp）")
	}
	for _, v := range []string{a.AccessToken, a.RefreshToken, a.UID, a.MachineID, a.DeviceID} {
		if strings.ContainsAny(v, "\r\n\x00") || len(v) > 32768 {
			return nil, provider.Identity{}, fmt.Errorf("Trae 凭据包含非法字符或超长字段")
		}
	}
	if a.MachineID == "" || a.DeviceID == "" {
		return nil, provider.Identity{}, fmt.Errorf("Trae 凭据缺少 machine_id / device_id；请使用完整导出或浏览器授权")
	}
	out, _ := json.Marshal(a)
	return out, identity(a), nil
}
func identity(a Credentials) provider.Identity {
	return provider.Identity{ID: a.UID, Name: a.Nickname, Scope: "cn", ExpiresAt: a.ExpiresAt, HasRefresh: a.RefreshToken != ""}
}
func decode(raw json.RawMessage) (Credentials, error) {
	var a Credentials
	e := json.Unmarshal(raw, &a)
	return a, e
}
func (c *Client) headers(r *http.Request, a Credentials, kind string) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "Trae/"+IDEVersion)
	if kind == "oauth" {
		return
	}
	if kind == "identity" {
		r.Header.Set("X-Cloudide-Token", a.AccessToken)
		return
	}
	r.Header.Set("Authorization", "Cloud-IDE-JWT "+a.AccessToken)
	if kind == "credit" {
		r.Header.Set("X-User-Region", "CN")
		r.Header.Set("X-Device-Id", a.DeviceID)
		return
	}
	for k, v := range map[string]string{"X-Cloudide-Token": a.AccessToken, "X-Ide-Token": a.AccessToken, "X-Uid": a.UID, "X-App-Id": AppID, "X-App-Version": "default", "X-Ide-Version": IDEVersion, "X-Ide-Version-Code": "20260811", "X-App-Version-Code": "20260811", "X-Ide-Version-Type": "stable", "X-Device-Type": "windows", "X-OS-Version": "Windows 11 Pro", "X-Device-Brand": "83DG", "Request-Traffic-Type": "prod", "X-Machine-Id": a.MachineID, "X-Device-Id": a.DeviceID} {
		if v != "" {
			r.Header.Set(k, v)
		}
	}
}
func errorFrom(status int, raw []byte, force bool) error {
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	code := text(obj, "code", "Code", "error_code")
	if meta := obj["ResponseMetadata"]; len(meta) > 0 {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(meta, &m)
		var e map[string]json.RawMessage
		if json.Unmarshal(m["Error"], &e) == nil && e != nil {
			code = text(e, "Code")
			force = true
		}
	}
	if status >= 200 && status < 300 && (code == "" || code == "0") && !force {
		return nil
	}
	// Never forward upstream text, request bodies or token-bearing callback URLs.
	if len(code) > 64 {
		code = "unknown"
	}
	for _, ch := range code {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '.' || ch == '_' || ch == '-') {
			code = "unknown"
			break
		}
	}
	if status < 400 {
		status = 502
	}
	fault := status == 401 || code == "1001" || strings.Contains(strings.ToLower(code), "token")
	if code == "1005" {
		fault = true
		status = 402
	}
	if code == "2001" {
		status = 403
	}
	if code == "11101" {
		status = 400
	}
	return &provider.Error{Status: status, Code: code, AccountFault: fault, Message: fmt.Sprintf("Trae 上游请求失败（HTTP %d，业务码 %s）", status, code)}
}
func (c *Client) request(ctx context.Context, a Credentials, kind, endpoint string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("Trae 请求地址无效")
	}
	c.headers(req, a, kind)
	if kind == "stream" {
		req.Header.Set("Accept", "text/event-stream")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Trae 网络请求失败：%T", err)
	}
	return resp, nil
}
func (c *Client) doJSON(ctx context.Context, a Credentials, kind, endpoint string, body any) (json.RawMessage, error) {
	resp, err := c.request(ctx, a, kind, endpoint, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 4<<20 {
		return nil, fmt.Errorf("Trae JSON 响应超过上限")
	}
	if err = errorFrom(resp.StatusCode, raw, false); err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("Trae JSON 响应无效")
	}
	return raw, nil
}
func (c *Client) user(ctx context.Context, a Credentials) (provider.Identity, error) {
	raw, err := c.doJSON(ctx, a, "identity", c.OAuthBase+UserPath, map[string]any{"ReqSource": "IDE", "IDEVersion": IDEVersion})
	if err != nil {
		return provider.Identity{}, err
	}
	var obj struct {
		Result map[string]json.RawMessage `json:"Result"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return provider.Identity{}, fmt.Errorf("Trae 账号信息无效")
	}
	id := text(obj.Result, "UserID")
	if id == "" {
		return provider.Identity{}, fmt.Errorf("Trae 未返回账号身份")
	}
	if a.UID != "" && a.UID != id {
		return provider.Identity{}, fmt.Errorf("Trae 账号身份不匹配")
	}
	a.UID = id
	a.Nickname = text(obj.Result, "ScreenName")
	return identity(a), nil
}
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return provider.Identity{}, err
	}
	a, _ := decode(normalized)
	return c.user(ctx, a)
}
func (c *Client) exchange(ctx context.Context, a Credentials) (Credentials, error) {
	if a.RefreshToken == "" {
		return a, &provider.Error{Status: 401, AccountFault: true, Message: "Trae 缺少续期令牌，请重新授权"}
	}
	raw, err := c.doJSON(ctx, a, "oauth", c.OAuthBase+ExchangePath, map[string]string{"ClientID": a.ClientID, "RefreshToken": a.RefreshToken, "ClientSecret": "-", "UserID": ""})
	if err != nil {
		return a, err
	}
	var obj struct {
		Result map[string]json.RawMessage `json:"Result"`
	}
	if json.Unmarshal(raw, &obj) != nil || obj.Result == nil {
		return a, fmt.Errorf("Trae 续期数据无效")
	}
	token := text(obj.Result, "Token")
	exp := expiry(text(obj.Result, "TokenExpireAt"))
	if exp <= time.Now().Unix() {
		seconds := expiry(text(obj.Result, "TokenExpireDuration"))
		if seconds > 0 && seconds <= 31536000 {
			exp = time.Now().Unix() + seconds
		}
	}
	if token == "" || exp <= time.Now().Unix() {
		return a, fmt.Errorf("Trae 未返回有效令牌或过期时间")
	}
	a.AccessToken = token
	a.ExpiresAt = exp
	if rt := text(obj.Result, "RefreshToken"); rt != "" {
		a.RefreshToken = rt
	}
	return a, nil
}
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	normalized, _, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	a, _ := decode(normalized)
	a, err = c.exchange(ctx, a)
	if err != nil {
		return nil, err
	}
	// The refresh token is already bound to the verified account. Return the
	// rotated pair immediately so a later profile-network failure cannot lose
	// the only valid refresh token. Import/login perform identity verification.
	return json.Marshal(a)
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	a, err := decode(raw)
	if err != nil {
		return nil, err
	}
	data, err := c.doJSON(ctx, a, "agent", c.AgentBase+ModelsPath, map[string]any{"function": "solo_work_lite", "config_names": nil, "need_prompt": false, "current_config_info": nil, "poly_prompt": true, "mode_type": nil, "agent_type": nil})
	if err != nil {
		return nil, err
	}
	var obj struct {
		List []struct {
			ID      string `json:"config_name"`
			Display struct {
				Name string `json:"display_name"`
			} `json:"display_config"`
		} `json:"config_info_list"`
	}
	data = unwrapData(data, "config_info_list")
	if json.Unmarshal(data, &obj) != nil {
		return nil, fmt.Errorf("Trae 模型目录无效")
	}
	out := []provider.Model{}
	seen := map[string]bool{}
	for _, m := range obj.List {
		if m.ID == "" || len(m.ID) > 200 || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		name := m.Display.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, provider.Model{ID: m.ID, UpstreamID: m.ID, Provider: provider.Trae, Scope: "cn", Name: name, Source: "upstream", StreamOnly: true, Desc: "Trae CN / SOLO 原生通道；上下文上限未验证"})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Trae 未返回可用模型配置，请核对账号权益")
	}
	return out, nil
}
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, err := decode(raw)
	if err != nil {
		return nil, err
	}
	data, err := c.doJSON(ctx, a, "credit", c.CreditBase+CreditPath, map[string]any{})
	if err != nil {
		return nil, err
	}
	return parseCreditInfo(data)
}

// Summary amounts only have a known credit unit when the upstream explicitly
// identifies credit billing. A capability-only entitlement must not invalidate
// an authoritative summary, and dollars must never be displayed as credits.
func parseCreditInfo(data json.RawMessage) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || root == nil {
		return nil, fmt.Errorf("Trae 积分数据无效")
	}
	_, hasSummary := root["usage_summary"]
	_, hasPacks := root["user_entitlement_pack_list"]
	if !hasSummary && !hasPacks {
		var nested map[string]json.RawMessage
		if json.Unmarshal(root["data"], &nested) == nil && nested != nil {
			data = root["data"]
		}
	}
	var obj struct {
		CreditsBilling *bool `json:"is_credits_billing"`
		DollarBilling  *bool `json:"is_dollar_usage_billing"`
		Summary        *struct {
			Total *float64 `json:"total_amount"`
			Used  *float64 `json:"consumed_amount"`
		} `json:"usage_summary"`
		Packs []struct {
			Base struct {
				Quota struct {
					Limit *float64 `json:"credits_limit"`
				} `json:"quota"`
			} `json:"entitlement_base_info"`
			Usage struct {
				Used *float64 `json:"credits_amount"`
			} `json:"usage"`
		} `json:"user_entitlement_pack_list"`
	}
	if json.Unmarshal(data, &obj) != nil {
		return nil, fmt.Errorf("Trae 积分数据无效")
	}
	total, used := 0.0, 0.0
	known := false
	reason := "上游未返回完整可计量的积分数据；未将未知或无限额度视为零"
	incompatibleUnit := (obj.CreditsBilling != nil && !*obj.CreditsBilling) || (obj.DollarBilling != nil && *obj.DollarBilling)
	if incompatibleUnit {
		reason = "上游返回非积分或混合计费数据，不能将金额直接显示为积分"
	} else if obj.CreditsBilling != nil && *obj.CreditsBilling && obj.Summary != nil && ((obj.Summary.Total != nil && *obj.Summary.Total < 0) || (obj.Summary.Used != nil && *obj.Summary.Used < 0)) {
		reason = "上游汇总额度不可计量，未将无限或异常额度显示为有限积分"
	} else if obj.CreditsBilling != nil && *obj.CreditsBilling && obj.Summary != nil && obj.Summary.Total != nil && obj.Summary.Used != nil && *obj.Summary.Total >= 0 && *obj.Summary.Used >= 0 {
		// The summary is authoritative and already includes applicable credit packs.
		// Do not add package limits again or infer missing per-package usage as zero.
		total, used, known = *obj.Summary.Total, *obj.Summary.Used, true
	} else {
		known = len(obj.Packs) > 0
		for _, p := range obj.Packs {
			if p.Base.Quota.Limit == nil || p.Usage.Used == nil || *p.Base.Quota.Limit <= 0 || *p.Usage.Used < 0 {
				known = false
				continue
			}
			total += *p.Base.Quota.Limit
			used += *p.Usage.Used
		}
	}
	if known {
		reason = ""
	}
	return json.Marshal(map[string]any{"available": known, "total": total, "used": used, "remaining": max(0, total-used), "package_name": "Trae", "status": "unknown", "reason": reason})
}

var _ provider.Adapter = (*Client)(nil)

func unwrapData(raw json.RawMessage, field string) json.RawMessage {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return raw
	}
	if _, ok := root[field]; ok {
		return raw
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(root["data"], &data) == nil {
		if _, ok := data[field]; ok {
			return root["data"]
		}
	}
	return raw
}
