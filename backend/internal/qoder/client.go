// Package qoder implements Qoder's native account and COSY inference protocols.
// It does not run the CLI, execute tools, or read desktop credentials.
package qoder

import (
	"bytes"
	"context"
	"crypto/rand"
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

const Version = "1.1.81"
const ModelsPath = "/algo/api/v2/model/list?Encode=1"
const ChatPath = "/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"

type Region struct{ Web, OpenAPI, Inference, ClientID, Redirect string }

type Credentials struct {
	AccessToken      string   `json:"access_token"`
	RefreshToken     string   `json:"refresh_token,omitempty"`
	ExpiresAt        int64    `json:"expires_at"`
	UID              string   `json:"uid"`
	Name             string   `json:"name,omitempty"`
	Scope            string   `json:"scope"`
	MachineID        string   `json:"machine_id"`
	UserType         string   `json:"user_type,omitempty"`
	OrganizationID   string   `json:"organization_id,omitempty"`
	OrganizationName string   `json:"organization_name,omitempty"`
	OrganizationTags []string `json:"organization_tags,omitempty"`
}

type Client struct {
	HTTP    *http.Client
	Regions map[string]Region
	mu      sync.Mutex
	logins  map[string]pendingLogin
}

func New() *Client {
	return &Client{
		HTTP: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		Regions: map[string]Region{
			"cn":     {"https://qoder.com.cn", "https://openapi.qoder.com.cn", "https://gateway.qoder.com.cn", "1c5e33e1-364d-4ce6-b02c-acaa81274a5c", "qoder-work-cn://"},
			"global": {"https://qoder.com", "https://openapi.qoder.sh", "https://api3.qoder.sh", "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb", ""},
		},
	}
}

func (*Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: provider.Qoder, Name: "Qoder", Available: true, Scopes: []string{"cn", "global"}, Login: true}
}

func stringField(obj map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		var s string
		if json.Unmarshal(obj[name], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

func timestamp(raw json.RawMessage) int64 {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		s = string(raw)
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1e12 {
			n /= 1000
		}
		return n
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.Unix()
	}
	return 0
}

func (*Client) Normalize(raw json.RawMessage) (json.RawMessage, provider.Identity, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, provider.Identity{}, fmt.Errorf("Qoder 凭据必须为 JSON 对象")
	}
	a := Credentials{UID: stringField(obj, "uid"), Name: stringField(obj, "name", "nickname"), MachineID: stringField(obj, "machine_id"), UserType: stringField(obj, "user_type"), OrganizationID: stringField(obj, "organization_id"), OrganizationName: stringField(obj, "organization_name")}
	if b, ok := obj["organization_tags"]; ok && json.Unmarshal(b, &a.OrganizationTags) != nil {
		return nil, provider.Identity{}, fmt.Errorf("Qoder 组织标签格式无效")
	}
	a.AccessToken = stringField(obj, "access_token", "accessToken", "token")
	a.RefreshToken = stringField(obj, "refresh_token", "refreshToken")
	a.Scope = stringField(obj, "scope", "realm")
	if a.Scope == "intl" {
		a.Scope = "global"
	}
	for _, key := range []string{"expires_at", "expiresAt", "expire_time"} {
		if _, ok := obj[key]; ok {
			a.ExpiresAt = timestamp(obj[key])
			break
		}
	}
	if a.AccessToken == "" || a.UID == "" || a.ExpiresAt <= 0 || a.MachineID == "" || a.Scope != "cn" && a.Scope != "global" {
		return nil, provider.Identity{}, fmt.Errorf("Qoder 凭据需要 access_token、uid、expires_at、machine_id 和 scope（cn / global）")
	}
	if len(a.MachineID) > 256 || len(a.UID) > 200 || len(a.OrganizationTags) > 64 {
		return nil, provider.Identity{}, fmt.Errorf("Qoder 身份字段过长")
	}
	fields := append([]string{a.AccessToken, a.RefreshToken, a.UID, a.Name, a.MachineID, a.UserType, a.OrganizationID, a.OrganizationName}, a.OrganizationTags...)
	for _, value := range fields {
		if len(value) > 32768 || strings.ContainsAny(value, "\r\n\x00") {
			return nil, provider.Identity{}, fmt.Errorf("Qoder 凭据包含非法字符或超长字段")
		}
	}
	normalized, err := json.Marshal(a)
	return normalized, identity(a), err
}

func identity(a Credentials) provider.Identity {
	return provider.Identity{ID: a.UID, Name: a.Name, Scope: a.Scope, ExpiresAt: a.ExpiresAt, HasRefresh: a.RefreshToken != ""}
}

func decode(raw json.RawMessage) (Credentials, error) {
	var a Credentials
	err := json.Unmarshal(raw, &a)
	return a, err
}

func uuid() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("Qoder 随机源不可用")
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}

func failure(status int) error {
	if status < 400 || status > 599 {
		status = 502
	}
	return &provider.Error{Status: status, AccountFault: status == 401, Message: fmt.Sprintf("Qoder 上游请求失败（HTTP %d）", status)}
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, &provider.Error{Status: 502, Message: "Qoder 上游连接失败"}
	}
	return resp, nil
}

func readJSON(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, failure(resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(b) > 4<<20 || !json.Valid(b) {
		return nil, &provider.Error{Status: 502, Message: "Qoder 返回了无效或过大的 JSON 响应"}
	}
	return b, nil
}

func (c *Client) authRequest(ctx context.Context, scope, path, method, token string, body []byte) (*http.Response, error) {
	r, ok := c.Regions[scope]
	if !ok {
		return nil, fmt.Errorf("Qoder 地区无效")
	}
	req, err := http.NewRequestWithContext(ctx, method, r.OpenAPI+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Qoder 请求地址无效")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "qoder/"+Version)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return c.do(req)
}

// Credit deliberately reports unknown until a verified quota contract is added.
// Unknown quota must not be represented as zero usage or free inference.
func (*Client) Credit(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"available":false,"reason":"Qoder 积分查询尚未接入；以官方账号页面为准"}`), nil
}
