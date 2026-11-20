package chatgpt

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (*Client) ResolveProtocol(_ json.RawMessage, _ provider.Model, _ provider.Protocol) (provider.Protocol, error) {
	return provider.ResponsesProtocol, nil
}
func (c *Client) StreamNative(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	a, err := decode(raw)
	if err != nil {
		return err
	}
	if r.Protocol != provider.ResponsesProtocol || !providerhttp.ModelID(r.Model) {
		return providerhttp.Invalid("ChatGPT", "协议或型号无效")
	}
	body, err := providerhttp.NativeBody(r, a.AuthMode == "codex")
	if err != nil {
		return err
	}
	endpoint := c.API + "/responses"
	if a.AuthMode == "codex" {
		endpoint = c.Backend + "/responses"
	}
	h := headers(a)
	h.Set("Accept", "text/event-stream")
	if r.SessionID != "" {
		h.Set("Session_id", r.SessionID)
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "ChatGPT", http.MethodPost, endpoint, h, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("ChatGPT", resp)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return providerhttp.Broken("ChatGPT")
	}
	return providerhttp.NativeSSE(ctx, resp.Body, r.Protocol, "ChatGPT", emit)
}
