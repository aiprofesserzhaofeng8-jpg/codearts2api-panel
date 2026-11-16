package commandcode

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
	if !validModel(r.Model) {
		return providerhttp.Invalid("Command Code", "型号无效")
	}
	p := nativePath(r.Model)
	if p == "" {
		return providerhttp.Invalid("Command Code", "暂不支持该型号的原生协议")
	}
	var body any
	switch p {
	case "/chat/completions":
		body, err = chatwire.EncodeQualified(r, "Command Code")
	case "/responses":
		body, err = responsewire.Encode(r, false, "Command Code")
	case "/messages":
		body, err = messagewire.Encode(r, "Command Code")
	}
	if err != nil {
		return err
	}
	h := http.Header{"Authorization": {"Bearer " + a.Key}, "User-Agent": {"codearts2api-panel/commandcode"}, "Accept": {"text/event-stream"}}
	if p == "/messages" {
		h.Set("anthropic-version", "2023-06-01")
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "Command Code", "POST", c.Origin+"/provider/v1"+p, h, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return providerhttp.FailureResponse("Command Code", resp)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken("Command Code")
	}
	switch p {
	case "/chat/completions":
		return chatwire.Decode(ctx, resp.Body, "Command Code", emit)
	case "/responses":
		return responsewire.Decode(ctx, resp.Body, r.Model, "Command Code", emit)
	default:
		return messagewire.Decode(ctx, resp.Body, r.Model, "Command Code", emit)
	}
}

// Verify intentionally performs one short authenticated inference. A public
// catalog response is not proof of a valid key. The import UI must disclose this.
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	_, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	// Free models may not authenticate the key; require the authenticated account
	// endpoint as well as a successful inference in the selected catalog.
	who, err := c.get(ctx, raw, "/alpha/whoami")
	if err != nil {
		return id, err
	}
	var user map[string]json.RawMessage
	if json.Unmarshal(who["user"], &user) != nil || len(user) == 0 {
		return id, providerhttp.Broken("Command Code")
	}
	models, err := c.Models(ctx, raw)
	if err != nil {
		return id, err
	}
	if len(models) == 0 {
		return id, providerhttp.Invalid("Command Code", "没有可验证的型号")
	}
	// Prefer a small model when published; do not retry across paid models.
	chosen := models[0].ID
	for _, m := range models {
		if m.ID == "deepseek/deepseek-v4-flash" {
			chosen = m.ID
			break
		}
	}
	err = c.Stream(ctx, raw, provider.Request{Model: chosen, Body: []byte(`{"messages":[{"role":"user","content":"Reply OK"}],"max_tokens":64}`)}, func([]byte) error { return nil })
	return id, err
}
func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "commandcode", Name: "Command Code", Scopes: []string{"global"}, Available: true}
}

var _ provider.Adapter = (*Client)(nil)
