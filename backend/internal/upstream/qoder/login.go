package qoder

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codearts2api/internal/provider"
)

type pendingLogin struct {
	Scope, Nonce, Verifier, MachineID string
	Created                           time.Time
}

func (c *Client) BeginLogin(ctx context.Context, scope string) (provider.Login, error) {
	if err := ctx.Err(); err != nil {
		return provider.Login{}, err
	}
	r, ok := c.Regions[scope]
	if !ok {
		return provider.Login{}, fmt.Errorf("Qoder 地区须为 cn 或 global")
	}
	machine, err := uuid()
	if err != nil {
		return provider.Login{}, err
	}
	nonce, err := uuid()
	if err != nil {
		return provider.Login{}, err
	}
	if scope == "global" {
		nonce = strings.ReplaceAll(nonce, "-", "")
	}
	var entropy [48]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return provider.Login{}, fmt.Errorf("Qoder 随机源不可用")
	}
	verifier := base64.RawURLEncoding.EncodeToString(entropy[:])
	digest := sha256.Sum256([]byte(verifier))
	q := url.Values{"nonce": {nonce}, "challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "challenge_method": {"S256"}, "client_id": {r.ClientID}, "machine_id": {machine}}
	if r.Redirect != "" {
		q.Set("redirect_uri", r.Redirect)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.logins == nil {
		c.logins = map[string]pendingLogin{}
	}
	for id, pending := range c.logins {
		if time.Since(pending.Created) > 10*time.Minute {
			delete(c.logins, id)
		}
	}
	if len(c.logins) >= 128 {
		return provider.Login{}, fmt.Errorf("Qoder 未完成的授权过多")
	}
	c.logins[nonce] = pendingLogin{Scope: scope, Nonce: nonce, Verifier: verifier, MachineID: machine, Created: time.Now()}
	return provider.Login{State: nonce, URL: r.Web + "/device/selectAccounts?" + q.Encode()}, nil
}

func (c *Client) ReleaseLogin(state string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.logins, state)
}

func (c *Client) CheckLogin(ctx context.Context, scope, state string) (json.RawMessage, bool, error) {
	c.mu.Lock()
	pending, ok := c.logins[state]
	c.mu.Unlock()
	if !ok || pending.Scope != scope || time.Since(pending.Created) > 10*time.Minute {
		return nil, false, fmt.Errorf("Qoder 授权已失效，请重新生成链接")
	}
	q := url.Values{"nonce": {pending.Nonce}, "verifier": {pending.Verifier}, "challenge_method": {"S256"}}
	resp, err := c.authRequest(ctx, scope, "/api/v1/deviceToken/poll?"+q.Encode(), http.MethodGet, "", nil)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode == 202 || resp.StatusCode == 404 {
		resp.Body.Close()
		return nil, false, nil
	}
	raw, err := readJSON(resp)
	if err != nil {
		return nil, false, err
	}
	a := Credentials{Scope: scope, MachineID: pending.MachineID}
	if err := applyToken(&a, raw); err != nil {
		return nil, false, err
	}
	if err = c.loadProfile(ctx, &a); err != nil {
		return nil, false, err
	}
	b, err := json.Marshal(a)
	return b, err == nil, err
}

func applyToken(a *Credentials, raw []byte) error {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return failure(502)
	}
	token := stringField(obj, "token", "device_token", "access_token")
	if token == "" || len(token) > 32768 || strings.ContainsAny(token, "\r\n\x00") {
		return &provider.Error{Status: 502, Message: "Qoder 授权响应缺少有效令牌"}
	}
	expires := timestamp(obj["expires_at"])
	if expires <= 0 {
		// Qoder device/job token endpoints express expires_in in milliseconds.
		var ms int64
		if json.Unmarshal(obj["expires_in"], &ms) == nil && ms > 0 && ms <= int64((366*24*time.Hour)/time.Millisecond) {
			expires = time.Now().Add(time.Duration(ms) * time.Millisecond).Unix()
		}
	}
	if expires <= time.Now().Unix() {
		return &provider.Error{Status: 502, Message: "Qoder 授权响应缺少有效过期时间"}
	}
	a.AccessToken, a.ExpiresAt = token, expires
	if refresh := stringField(obj, "refresh_token"); refresh != "" {
		a.RefreshToken = refresh
	}
	return nil
}

func (c *Client) loadProfile(ctx context.Context, a *Credentials) error {
	resp, err := c.authRequest(ctx, a.Scope, "/api/v1/userinfo", http.MethodGet, a.AccessToken, nil)
	if err != nil {
		return err
	}
	raw, err := readJSON(resp)
	if err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return failure(502)
	}
	id := stringField(obj, "id", "uid", "user_id")
	if id == "" || a.UID != "" && a.UID != id {
		return &provider.Error{Status: 401, AccountFault: true, Message: "Qoder 上游身份与凭据不一致"}
	}
	a.UID, a.Name = id, stringField(obj, "name", "nickname")
	a.UserType = stringField(obj, "user_type")
	a.OrganizationID = stringField(obj, "organization_id", "organizationId")
	a.OrganizationName = stringField(obj, "organization_name", "organizationName")
	if b, ok := obj["organization_tags"]; ok {
		if json.Unmarshal(b, &a.OrganizationTags) != nil {
			return failure(502)
		}
	}
	return nil
}

func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	a, err := decode(raw)
	if err == nil {
		err = c.loadProfile(ctx, &a)
	}
	return identity(a), err
}

func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	a, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if a.RefreshToken == "" {
		return nil, &provider.Error{Status: 401, AccountFault: true, Message: "Qoder 凭据不能续期，请重新登录"}
	}
	path := "/api/v1/deviceToken/refresh"
	if strings.HasPrefix(a.RefreshToken, "jrt-") {
		path = "/api/v1/jobToken/refresh"
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": a.RefreshToken})
	resp, err := c.authRequest(ctx, a.Scope, path, http.MethodPost, "", body)
	if err != nil {
		return nil, err
	}
	b, err := readJSON(resp)
	if err != nil {
		return nil, err
	}
	if err = applyToken(&a, b); err != nil {
		return nil, err
	}
	// Do not add a fallible profile call after successful token rotation. The
	// existing credentials service must commit the new generation immediately.
	return json.Marshal(a)
}
