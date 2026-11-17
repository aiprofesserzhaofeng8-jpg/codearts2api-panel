package claude

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (*Client) ResolveProtocol(_ json.RawMessage, _ provider.Model, _ provider.Protocol) (provider.Protocol, error) {
	return provider.MessagesProtocol, nil
}
func (c *Client) StreamNative(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	a, err := decode(raw)
	if err != nil {
		return err
	}
	if r.Protocol != provider.MessagesProtocol || !providerhttp.ModelID(r.Model) {
		return providerhttp.Invalid("Claude", "协议或型号无效")
	}
	body, err := providerhttp.NativeBody(r, false)
	if err != nil {
		return err
	}
	h := headers(a)
	h.Set("Accept", "text/event-stream")
	endpoint := c.API + "/v1/messages"
	if a.AuthMode == "oauth" {
		endpoint += "?beta=true"
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "Claude", http.MethodPost, endpoint, h, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("Claude", resp)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return providerhttp.Broken("Claude")
	}
	return providerhttp.NativeSSE(ctx, resp.Body, r.Protocol, "Claude", emit)
}
