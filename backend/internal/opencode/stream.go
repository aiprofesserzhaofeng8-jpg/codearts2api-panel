package opencode

import (
	"codearts2api/internal/chatwire"
	"codearts2api/internal/messagewire"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"codearts2api/internal/responsewire"
	"context"
	"encoding/json"
	"mime"
	"net/http"
)

func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	if !providerhttp.ModelID(r.Model) {
		return providerhttp.Invalid("OpenCode", "型号无效")
	}
	p, ok := modelProtocol(a.Scope, r.Model)
	if !ok {
		return providerhttp.Invalid("OpenCode", "暂不支持该型号的原生协议")
	}
	var body any
	switch p {
	case chat:
		body, err = chatwire.Encode(r, "OpenCode")
	case responses:
		body, err = responsewire.Encode(r, false, "OpenCode")
	case messages:
		body, err = messagewire.Encode(r, "OpenCode")
	}
	if err != nil {
		return err
	}
	h := http.Header{"Authorization": {"Bearer " + a.Key}, "User-Agent": {"codearts2api-panel/opencode"}, "Accept": {"text/event-stream"}}
	if r.SessionID != "" {
		if !providerhttp.Safe(r.SessionID) {
			return providerhttp.Invalid("OpenCode", "会话标识无效")
		}
		h.Set("x-opencode-session", r.SessionID)
	}
	if p == messages {
		h.Set("anthropic-version", "2023-06-01")
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "OpenCode", "POST", c.base(a.Scope)+protocolPath(p), h, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return providerhttp.FailureResponse("OpenCode", resp)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken("OpenCode")
	}
	switch p {
	case chat:
		return chatwire.Decode(ctx, resp.Body, "OpenCode", emit)
	case responses:
		return responsewire.Decode(ctx, resp.Body, r.Model, "OpenCode", emit)
	default:
		return messagewire.Decode(ctx, resp.Body, r.Model, "OpenCode", emit)
	}
}

// Verify intentionally performs one short authenticated inference. A public
// catalog response is not proof of a valid key. The import UI must disclose this.
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	_, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	models, err := c.Models(ctx, raw)
	if err != nil {
		return id, err
	}
	if len(models) == 0 {
		return id, providerhttp.Invalid("OpenCode", "没有可验证的型号")
	}
	// Prefer a small model when published; do not retry across paid models.
	chosen := models[0].ID
	for _, m := range models {
		if m.ID == "minimax-m3.1-flash" {
			chosen = m.ID
			break
		}
	}
	err = c.Stream(ctx, raw, provider.Request{Model: chosen, Body: []byte(`{"messages":[{"role":"user","content":"Reply OK"}],"max_tokens":64}`)}, func([]byte) error { return nil })
	return id, err
}
func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "opencode", Name: "OpenCode Zen / Go", Scopes: []string{"zen", "go"}, Available: true}
}

var _ provider.Adapter = (*Client)(nil)
