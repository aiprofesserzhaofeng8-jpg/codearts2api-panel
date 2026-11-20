// SPDX-License-Identifier: LGPL-3.0-only
// Adapted Sub2API protocol mappings; see UPSTREAM.md.
package gemini

import (
	"codearts2api/internal/googlewire"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"mime"
	"net/http"
)

func nativeRequest(body []byte, model string) (json.RawMessage, error) {
	return googlewire.Encode(body, model)
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	if !isCLI(raw) {
		return c.Client.Stream(ctx, raw, r, emit)
	}
	b, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	var a cliCredentials
	_ = json.Unmarshal(b, &a)
	if !providerhttp.ModelID(r.Model) {
		return providerhttp.Invalid("Gemini", "型号无效")
	}
	request, err := nativeRequest(r.Body, r.Model)
	if err != nil {
		return err
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "Gemini", http.MethodPost, c.CLIBase+"/v1internal:streamGenerateContent?alt=sse", http.Header{"Authorization": {"Bearer " + a.Access}, "User-Agent": {GeminiCLIUserAgent}}, map[string]any{"model": r.Model, "project": a.Project, "request": request})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("Gemini", resp)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken("Gemini")
	}
	return googlewire.Decode(ctx, resp.Body, "Gemini", r.Model, emit)
}
