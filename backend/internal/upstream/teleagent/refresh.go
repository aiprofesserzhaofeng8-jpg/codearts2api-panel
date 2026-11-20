package teleagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

const RefreshPath = "/superCowork/sapi/cowork/auth/token-refresh"

// The official desktop's ordinary browser login rotates its access token.
// Imported or KMS-managed sessions must not opt into this protocol implicitly.
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	normalized, before, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	a, _ := decode(normalized)
	if !a.Renewable {
		return nil, invalid("此账号不是独立浏览器登录，请重新登录；不自动轮转桌面或 KMS 共享凭据")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	nonce[6] = (nonce[6] & 0x0f) | 0x40
	nonce[8] = (nonce[8] & 0x3f) | 0x80
	osType := runtime.GOOS
	if osType == "windows" {
		osType = "Windows"
	}
	if osType == "darwin" {
		osType = "macOS"
	}
	h := http.Header{"X-Token": {a.Token}, "X-Timestamp": {strconv.FormatInt(time.Now().Unix(), 10)}, "X-Nonce": {fmt.Sprintf("%x-%x-%x-%x-%x", nonce[:4], nonce[4:6], nonce[6:8], nonce[8:10], nonce[10:])}, "X-App-Version": {a.AppVersion}, "X-OS-Type": {osType}, "X-SuperAgent-Device-Id": {a.DeviceID}, "Content-Type": {"application/json"}}
	resp, err := providerhttp.Do(ctx, c.HTTP, "TeleAgent", "POST", c.Origin+RefreshPath, h, nil)
	if err != nil {
		return nil, err
	}
	data, err := providerhttp.JSON(resp, "TeleAgent")
	if err != nil {
		return nil, err
	}
	var nested map[string]json.RawMessage
	_ = json.Unmarshal(data["data"], &nested)
	for _, layer := range []map[string]json.RawMessage{data, nested} {
		code := providerhttp.Field(layer, "responseCode", "code")
		if code == "" {
			code = string(layer["responseCode"])
			if code == "" {
				code = string(layer["code"])
			}
		}
		if code == "50005" {
			return nil, &provider.Error{Status: 503, Message: "TeleAgent 续期服务暂不可用，请稍后重试"}
		}
		if n, _ := strconv.Atoi(code); n >= 40112 && n <= 40116 {
			return nil, &provider.Error{Status: 401, AccountFault: true, Message: "TeleAgent 登录会话已失效，请重新登录"}
		}
		if code != "" && code != "null" && code != "0" && code != "200" {
			return nil, providerhttp.Broken("TeleAgent")
		}
	}
	if providerhttp.Field(nested, "accessToken") != "" {
		data = nested
	}
	token := providerhttp.Field(data, "accessToken")
	ttl := providerhttp.Timestamp(data["expiresIn"])
	if token == "" || ttl <= 0 || ttl > 90*24*3600 {
		return nil, providerhttp.Broken("TeleAgent")
	}
	a.Token, a.ExpiresAt = token, time.Now().Unix()+ttl
	if absolute := providerhttp.Timestamp(data["absoluteExpiresAt"]); absolute > 0 && absolute < a.ExpiresAt {
		a.ExpiresAt = absolute
	}
	updated, _ := json.Marshal(a)
	updated, after, err := c.Normalize(updated)
	if err != nil {
		return nil, err
	}
	if after.ID != before.ID || after.ExpiresAt <= time.Now().Unix() {
		return nil, providerhttp.Broken("TeleAgent")
	}
	return updated, nil
}
