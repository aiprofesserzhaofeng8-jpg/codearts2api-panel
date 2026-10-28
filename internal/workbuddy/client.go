// Package workbuddy implements the native WorkBuddy API. Endpoint/header shapes
// are based on linguo2625469/workbuddy2api-panel (MIT); see UPSTREAM.md.
package workbuddy

import (
	"bufio"
	"bytes"
	"codearts2api/internal/provider"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at"`
	UID          string `json:"uid"`
	Nickname     string `json:"nickname,omitempty"`
	Realm        string `json:"realm"`
	Domain       string `json:"domain,omitempty"`
	EnterpriseID string `json:"enterprise_id,omitempty"`
	DeviceToken  string `json:"device_token,omitempty"`
}
type Client struct {
	HTTP                                 *http.Client
	CNBase, GlobalBase, CNBilling, CNWeb string
}

func New() *Client {
	return &Client{HTTP: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, CNBase: "https://copilot.tencent.com", GlobalBase: "https://www.workbuddy.ai", CNBilling: "https://www.codebuddy.cn", CNWeb: "https://www.workbuddy.cn"}
}
func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.WorkBuddy, Name: "WorkBuddy", Available: true, Scopes: []string{"cn", "global"}, Login: true}
}
func randomID() string { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func (c *Client) base(realm string) string {
	if realm == "global" {
		return c.GlobalBase
	}
	return c.CNBase
}
func (c *Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return nil, provider.Identity{}, fmt.Errorf("WorkBuddy 凭据必须为 JSON 对象")
	}
	root := data
	if nested := data["auth"]; len(nested) > 0 && string(nested) != "null" {
		if json.Unmarshal(nested, &data) != nil {
			return nil, provider.Identity{}, fmt.Errorf("auth 凭据格式无效")
		}
	}
	get := func(keys ...string) string {
		for _, k := range keys {
			var v string
			if json.Unmarshal(data[k], &v) == nil && v != "" {
				return v
			}
		}
		return ""
	}
	a := Credentials{AccessToken: get("access_token", "accessToken"), RefreshToken: get("refresh_token", "refreshToken"), UID: get("uid", "user_id"), Nickname: get("nickname"), Realm: get("realm"), Domain: get("domain"), EnterpriseID: get("enterprise_id", "enterpriseId"), DeviceToken: get("device_token", "deviceToken")}
	if account := root["account"]; len(account) > 0 {
		var who struct {
			UID          string `json:"uid"`
			Nickname     string `json:"nickname"`
			EnterpriseID string `json:"enterpriseId"`
		}
		if json.Unmarshal(account, &who) != nil {
			return nil, provider.Identity{}, fmt.Errorf("WorkBuddy account 字段无效")
		}
		if a.UID == "" {
			a.UID = who.UID
		}
		if a.Nickname == "" {
			a.Nickname = who.Nickname
		}
		if a.EnterpriseID == "" {
			a.EnterpriseID = who.EnterpriseID
		}
	}
	if a.DeviceToken == "" {
		_ = json.Unmarshal(root["device_token"], &a.DeviceToken)
	}
	for _, k := range []string{"expires_at", "expiresAt", "expired_at"} {
		var n int64
		if json.Unmarshal(data[k], &n) == nil && n > 0 {
			a.ExpiresAt = n
			break
		}
	}
	if a.ExpiresAt > 1e12 {
		a.ExpiresAt /= 1000
	}
	if a.Realm == "" {
		if strings.Contains(strings.ToLower(a.Domain), "workbuddy.ai") || strings.Contains(strings.ToLower(a.Domain), "codebuddy.ai") {
			a.Realm = "global"
		} else {
			a.Realm = "cn"
		}
	}
	if a.Realm != "cn" && a.Realm != "global" {
		return nil, provider.Identity{}, fmt.Errorf("WorkBuddy 地区必须为 cn 或 global")
	}
	if a.AccessToken == "" || a.UID == "" || len(a.UID) > 200 || len(a.AccessToken) > 32768 {
		return nil, provider.Identity{}, fmt.Errorf("WorkBuddy 凭据缺少有效 access_token 或 uid")
	}
	if a.ExpiresAt == 0 {
		return nil, provider.Identity{}, fmt.Errorf("WorkBuddy 凭据缺少 expires_at（Unix 秒）")
	}
	out, _ := json.Marshal(a)
	return out, provider.Identity{ID: a.UID, Name: a.Nickname, Scope: a.Realm, ExpiresAt: a.ExpiresAt, HasRefresh: a.RefreshToken != ""}, nil
}
func decode(raw json.RawMessage) (Credentials, error) {
	var a Credentials
	err := json.Unmarshal(raw, &a)
	return a, err
}
func (c *Client) headers(r *http.Request, a Credentials) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-Requested-With", "XMLHttpRequest")
	r.Header.Set("X-CodeBuddy-Request", "1")
	r.Header.Set("User-Agent", "WorkBuddy/5.5.4 WorkBuddy/5.5.4 CLI/2.137.1")
	origin := "https://www.codebuddy.cn"
	if a.Realm == "global" {
		origin = "https://www.workbuddy.ai"
	}
	r.Header.Set("Origin", origin)
	r.Header.Set("Referer", origin+"/")
	if a.AccessToken != "" {
		r.Header.Set("Authorization", "Bearer "+a.AccessToken)
	}
	if a.UID != "" {
		r.Header.Set("X-User-Id", a.UID)
		for _, s := range []string{"machine", "session"} {
			sum := sha256.Sum256([]byte("wb2a:" + s + ":" + a.UID))
			r.Header.Set("X-"+strings.Title(s)+"-ID", hex.EncodeToString(sum[:18]))
		}
	}
	if a.EnterpriseID != "" {
		r.Header.Set("X-Enterprise-Id", a.EnterpriseID)
		r.Header.Set("X-Tenant-Id", a.EnterpriseID)
	} else {
		r.Header.Set("X-No-Enterprise-Id", "1")
	}
	if a.Realm == "global" {
		r.Header.Set("X-Domain", "www.workbuddy.ai")
	} else if a.Domain != "" {
		r.Header.Set("X-Domain", a.Domain)
	} else {
		r.Header.Set("X-No-Department-Info", "1")
	}
	if a.DeviceToken != "" {
		r.Header.Set("X-Device-Token", a.DeviceToken)
	}
	r.Header.Set("X-Product", "WorkBuddy")
	r.Header.Set("X-IDE-Name", "WorkBuddy")
	r.Header.Set("X-IDE-Type", "WorkBuddy")
	r.Header.Set("X-IDE-Version", "5.5.4")
}
func upstreamError(status int, raw []byte) error {
	var e struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"msg"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	code := strings.Trim(string(e.Code), "\"")
	if len(code) > 32 {
		code = "unknown"
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			code = "unknown"
			break
		}
	}
	if status < 300 && (code == "" || code == "0") && e.Error.Message == "" {
		return nil
	}
	// Never echo an arbitrary upstream body: it may contain bearer credentials.
	if status < 400 {
		status = 400
	}
	fault := status == 401 || status == 403 || code == "12153" || code == "14017"
	if code == "6004" {
		status = 429
	}
	if code == "6021" {
		status = 503
	}
	if code == "11102" || code == "11158" {
		status = 404
	}
	pending := strings.Contains(strings.ToLower(e.Message), "login ing") || strings.Contains(strings.ToLower(e.Message), "pending") || strings.Contains(e.Message, "未登录") || strings.Contains(e.Message, "等待")
	return &provider.Error{Status: status, Code: code, AccountFault: fault, Pending: pending, Message: fmt.Sprintf("WorkBuddy 上游拒绝请求（HTTP %d，业务码 %s）", status, code)}
}
func (c *Client) doJSON(ctx context.Context, a Credentials, method, endpoint string, body []byte, refresh bool) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.headers(req, a)
	if req.URL.Path == "/v3/config" {
		req.Header.Set("X-Product", "SaaS")
		req.Header.Set("User-Agent", "CodeBuddyIDE/4.12.0 CodeBuddy/4.12.0")
		if a.Domain == "" && a.Realm != "global" {
			req.Header.Set("X-Domain", "copilot.tencent.com")
		}
	}
	if req.URL.Path == "/console/account" {
		req.Header.Set("x-client-platform", "web")
	}
	if refresh {
		req.Header.Del("Authorization")
		req.Header.Set("X-Refresh-Token", a.RefreshToken)
		req.Header.Set("X-Auth-Refresh-Source", "plugin")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("WorkBuddy 请求失败：%T", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if err = upstreamError(resp.StatusCode, raw); err != nil {
		return nil, err
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &env) != nil || len(env.Data) == 0 {
		return nil, fmt.Errorf("WorkBuddy 返回了无效的数据结构")
	}
	return env.Data, nil
}
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if a.RefreshToken == "" {
		return nil, &provider.Error{Status: 401, AccountFault: true, Message: "WorkBuddy 凭据已过期，请重新登录"}
	}
	data, err := c.doJSON(ctx, a, "POST", c.base(a.Realm)+"/v2/plugin/auth/token/refresh", nil, true)
	if err != nil {
		return nil, err
	}
	var tok struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	if json.Unmarshal(data, &tok) != nil || tok.AccessToken == "" || tok.RefreshToken == "" || tok.ExpiresIn <= 0 || tok.ExpiresIn > 31536000 {
		return nil, fmt.Errorf("WorkBuddy 续期数据无效")
	}
	a.AccessToken = tok.AccessToken
	a.RefreshToken = tok.RefreshToken
	a.ExpiresAt = time.Now().Unix() + tok.ExpiresIn
	if tok.Domain != "" {
		a.Domain = tok.Domain
	}
	return json.Marshal(a)
}
func (c *Client) Stream(ctx context.Context, credentials json.RawMessage, input provider.Request, onChunk func([]byte) error) error {
	a, err := decode(credentials)
	if err != nil {
		return err
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(input.Body, &body) != nil {
		return &provider.Error{Status: 400, Message: "请求 JSON 无效"}
	}
	body["model"], _ = json.Marshal(input.Model)
	body["stream"] = json.RawMessage("true")
	body["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	if alias, ok := body["max_completion_tokens"]; ok {
		if _, set := body["max_tokens"]; !set {
			body["max_tokens"] = alias
		}
		delete(body, "max_completion_tokens")
	}
	if choice := body["tool_choice"]; len(choice) > 0 && bytes.HasPrefix(bytes.TrimSpace(choice), []byte("{")) {
		return &provider.Error{Status: 400, Message: "WorkBuddy 暂不支持指定函数形式的 tool_choice，请使用 auto"}
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.base(a.Realm)+"/v2/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.headers(req, a)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("X-Agent-Purpose", "conversation")
	normalizeID := func(value string) string {
		if value == "" {
			return randomID()
		}
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:16])
	}
	conv := normalizeID(input.SessionID)
	request := normalizeID(input.RequestID)
	id := randomID()
	for key, value := range map[string]string{"X-Conversation-ID": conv, "X-Conversation-Request-ID": request, "X-Conversation-Message-ID": id, "X-Root-Request-ID": request, "X-Request-ID": id, "X-Trace-ID": request, "X-B3-TraceId": id, "X-B3-SpanId": id[:16], "X-B3-Sampled": "1"} {
		req.Header.Set(key, value)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("WorkBuddy 连接失败：%T", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16384))
		return upstreamError(resp.StatusCode, raw)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 65536), 8<<20)
	seen := map[int]bool{}
	finished := map[int]bool{}
	done := false
	var event []string
	eventBytes := 0
	emit := func() error {
		if len(event) == 0 {
			return nil
		}
		raw := []byte(strings.Join(event, "\n"))
		event = nil
		eventBytes = 0
		if bytes.Equal(raw, []byte("[DONE]")) {
			done = true
			return nil
		}
		if err := upstreamError(200, raw); err != nil {
			return err
		}
		var chunk struct {
			Choices []struct {
				Index  int     `json:"index"`
				Finish *string `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(raw, &chunk) != nil {
			return fmt.Errorf("WorkBuddy 流式数据无效")
		}
		for _, ch := range chunk.Choices {
			seen[ch.Index] = true
			if ch.Finish != nil && *ch.Finish != "" {
				finished[ch.Index] = true
			}
		}
		if len(chunk.Choices) == 0 && len(chunk.Usage) == 0 {
			return nil
		}
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(raw, &obj)
		obj["model"], _ = json.Marshal(provider.ModelID(provider.WorkBuddy, a.Realm, input.Model))
		out, _ := json.Marshal(obj)
		return onChunk(out)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := emit(); err != nil {
				return err
			}
			if done {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			eventBytes += len(line)
			if eventBytes > 8<<20 {
				return fmt.Errorf("WorkBuddy 单个流式事件超过 8 MiB")
			}
			event = append(event, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := emit(); err != nil {
		return err
	}
	if len(seen) == 0 {
		return fmt.Errorf("WorkBuddy 返回空响应")
	}
	for i := range seen {
		if !finished[i] {
			return fmt.Errorf("WorkBuddy 响应在完成前中断")
		}
	}
	return nil
}
func (c *Client) Models(ctx context.Context, raw json.RawMessage) ([]provider.Model, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	data, e := c.doJSON(ctx, a, "GET", c.base(a.Realm)+"/v3/config", nil, false)
	if e != nil {
		return nil, e
	}
	var cfg struct {
		Models []struct {
			ID, Name    string
			Description string `json:"descriptionZh"`
			Credits     string `json:"credits"`
			MaxInput    int    `json:"maxInputTokens"`
			MaxOutput   int    `json:"maxOutputTokens"`
			Disabled    bool   `json:"disabled"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil, fmt.Errorf("WorkBuddy 模型目录无效")
	}
	var out []provider.Model
	for _, m := range cfg.Models {
		if m.ID != "" && !m.Disabled {
			out = append(out, provider.Model{ID: m.ID, UpstreamID: m.ID, Provider: provider.WorkBuddy, Scope: a.Realm, Name: m.Name, Desc: m.Description, Credit: m.Credits, Context: m.MaxInput, MaxOutput: m.MaxOutput, Source: "upstream", StreamOnly: true})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("WorkBuddy 未返回模型目录")
	}
	return out, nil
}
func (c *Client) Credit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, e := decode(raw)
	if e != nil {
		return nil, e
	}
	if a.EnterpriseID != "" {
		return json.Marshal(map[string]any{"available": false, "reason": "WorkBuddy 企业账号额度查询暂未接入，请到官方控制台核对"})
	}
	base, path := c.CNBilling, "/v2/billing/meter/get-user-resource"
	if a.Realm == "global" {
		base = c.GlobalBase
		path = "/billing/meter/get-user-resource"
	}
	now := time.Now()
	body, _ := json.Marshal(map[string]any{"PageNumber": 1, "PageSize": 100, "ProductCode": "p_tcaca", "Status": []int{0, 3}, "PackageEndTimeRangeBegin": now.Format("2006-01-02 15:04:05"), "PackageEndTimeRangeEnd": now.AddDate(100, 0, 0).Format("2006-01-02 15:04:05")})
	data, e := c.doJSON(ctx, a, "POST", base+path, body, false)
	if e != nil {
		return nil, e
	}
	var payload struct {
		Response struct {
			Data struct {
				Accounts []struct {
					Remain      int64 `json:"CapacityRemain"`
					Used        int64 `json:"CapacityUsed"`
					Size        int64 `json:"CapacitySize"`
					CycleRemain int64 `json:"CycleCapacityRemain"`
					CycleUsed   int64 `json:"CycleCapacityUsed"`
					CycleSize   int64 `json:"CycleCapacitySize"`
				} `json:"Accounts"`
			} `json:"Data"`
		} `json:"Response"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil, fmt.Errorf("WorkBuddy 额度数据无效")
	}
	if len(payload.Response.Data.Accounts) >= 100 {
		return json.Marshal(map[string]any{"available": false, "reason": "上游积分包达到单页上限，当前未汇总后续页，请在官方控制台核对完整额度"})
	}
	var total, remain, used int64
	for _, p := range payload.Response.Data.Accounts {
		if p.CycleSize > 0 {
			total += p.CycleSize
			remain += max(0, p.CycleRemain)
			used += max(0, p.CycleUsed)
		} else {
			total += p.Size
			remain += max(0, p.Remain)
			used += max(0, p.Used)
		}
	}
	return json.Marshal(map[string]any{"available": len(payload.Response.Data.Accounts) > 0, "total": total, "remaining": remain, "used": used, "status": "active", "package_name": "WorkBuddy", "reason": "上游未返回积分包"})
}
func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	if scope != "cn" && scope != "global" {
		return provider.Login{}, fmt.Errorf("无效地区")
	}
	data, e := c.doJSON(ctx, Credentials{Realm: scope}, "POST", c.base(scope)+"/v2/plugin/auth/state?platform=CLI", []byte(`{}`), false)
	if e != nil {
		return provider.Login{}, e
	}
	var r struct {
		State string `json:"state"`
		URL   string `json:"authUrl"`
	}
	if json.Unmarshal(data, &r) != nil || r.State == "" {
		return provider.Login{}, fmt.Errorf("WorkBuddy 未返回授权信息")
	}
	u, e := url.Parse(r.URL)
	if e != nil || u.Scheme != "https" || u.User != nil || !trustedLoginHost(u.Hostname()) {
		return provider.Login{}, fmt.Errorf("WorkBuddy 授权地址不在允许范围")
	}
	return provider.Login{State: r.State, URL: r.URL}, nil
}
func trustedLoginHost(h string) bool {
	for _, d := range []string{"workbuddy.cn", "workbuddy.ai", "codebuddy.cn", "codebuddy.ai", "tencent.com", "qq.com"} {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}
func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	data, e := c.doJSON(ctx, Credentials{Realm: scope}, "GET", c.base(scope)+"/v2/plugin/auth/token?state="+url.QueryEscape(state), nil, false)
	if e != nil {
		if p, ok := e.(*provider.Error); ok && (p.Pending || p.Code == "10101") {
			return nil, false, nil
		}
		return nil, false, e
	}
	var token struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	if json.Unmarshal(data, &token) != nil {
		return nil, false, fmt.Errorf("WorkBuddy 授权信息无效")
	}
	if token.AccessToken == "" {
		return nil, false, nil
	}
	if token.ExpiresIn <= 0 || token.ExpiresIn > 31536000 {
		return nil, false, fmt.Errorf("WorkBuddy 令牌有效期无效")
	}
	a := Credentials{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: time.Now().Unix() + token.ExpiresIn, Domain: token.Domain, Realm: scope}
	profile, e := c.doJSON(ctx, a, "GET", c.base(scope)+"/v2/plugin/login/account?state="+url.QueryEscape(state), nil, false)
	if e != nil {
		return nil, false, e
	}
	var who struct {
		UID          string `json:"uid"`
		Nickname     string `json:"nickname"`
		EnterpriseID string `json:"enterpriseId"`
	}
	if json.Unmarshal(profile, &who) != nil || who.UID == "" {
		return nil, false, fmt.Errorf("WorkBuddy 未返回账号身份")
	}
	a.UID = who.UID
	a.Nickname = who.Nickname
	a.EnterpriseID = who.EnterpriseID
	raw, _ := json.Marshal(a)
	_, _, e = c.Normalize(raw)
	return raw, e == nil, e
}

var _ provider.Adapter = (*Client)(nil)
var _ provider.BrowserLogin = (*Client)(nil)

func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	a, err := decode(raw)
	if err != nil {
		return provider.Identity{}, err
	}
	base := c.CNWeb
	if a.Realm == "global" {
		base = c.GlobalBase
	}
	data, err := c.doJSON(ctx, a, "GET", base+"/console/account", nil, false)
	if err != nil {
		return provider.Identity{}, err
	}
	var who struct {
		UID      string `json:"uid"`
		Nickname string `json:"nickname"`
	}
	if json.Unmarshal(data, &who) != nil || who.UID == "" || who.UID != a.UID {
		return provider.Identity{}, fmt.Errorf("WorkBuddy 账号身份核对失败")
	}
	return provider.Identity{ID: a.UID, Name: who.Nickname, Scope: a.Realm, ExpiresAt: a.ExpiresAt, HasRefresh: a.RefreshToken != ""}, nil
}
