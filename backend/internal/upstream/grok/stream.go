// SPDX-License-Identifier: LGPL-3.0-only
// Grok Build Responses endpoint/CLI identity follows Sub2API; see UPSTREAM.md.
package grok

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"codearts2api/internal/responsewire"
	"context"
	"encoding/json"
	"mime"
)

func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	if !providerhttp.ModelID(r.Model) {
		return providerhttp.Invalid("Grok", "型号无效")
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(r.Body, &input) != nil {
		return providerhttp.Invalid("Grok", "请求格式无效")
	}
	for _, key := range []string{"audio", "modalities", "prediction", "extra_body"} {
		if v := input[key]; len(v) > 0 && string(v) != "null" {
			return providerhttp.Invalid("Grok", "暂不支持参数 "+key)
		}
	}
	body, err := responsewire.Encode(r, false, "Grok")
	if err != nil {
		return err
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "Grok", "POST", c.BaseURL+"/responses", cliHeaders(a.Access), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("Grok", resp)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken("Grok")
	}
	return responsewire.Decode(ctx, resp.Body, r.Model, "Grok", emit)
}

func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "grok", Name: "Grok Build", Scopes: []string{"build"}, Login: true, Available: true}
}
