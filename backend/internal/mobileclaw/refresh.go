package mobileclaw

import (
	"context"
	"encoding/json"
	"time"

	"codearts2api/internal/providerhttp"
)

// Refresh uses the independent device created by the panel's QR login. Desktop
// exports do not contain this material, so their shared session is not rotated.
func (c *Client) Refresh(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	normalized, before, err := c.Normalize(raw)
	if err != nil {
		return nil, err
	}
	var a Credentials
	_ = json.Unmarshal(normalized, &a)
	if !before.HasRefresh {
		return nil, invalid("此账号没有独立续期凭据，请重新扫码登录")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	device := *a.RefreshDevice
	device.AIProvider = a.AIProvider
	response, err := c.loginPost(ctx, "refresh", device, a.RefreshToken)
	if err != nil {
		return nil, err
	}
	token := providerhttp.Field(response, "accessToken")
	refresh := providerhttp.Field(response, "refreshToken")
	if token == "" || refresh == "" {
		return nil, providerhttp.Broken("MobileClaw")
	}
	a.Token, a.RefreshToken = token, refresh
	// Keep the expected provider: Normalize rejects a lost or changed binding.
	updated, _ := json.Marshal(a)
	updated, after, err := c.Normalize(updated)
	if err != nil {
		return nil, err
	}
	if after.ID != before.ID || after.ExpiresAt <= time.Now().Unix() {
		return nil, providerhttp.Broken("MobileClaw")
	}
	return updated, nil
}
