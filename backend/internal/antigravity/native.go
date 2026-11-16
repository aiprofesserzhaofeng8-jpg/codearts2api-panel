// SPDX-License-Identifier: LGPL-3.0-only
// Antigravity envelope adapted from Sub2API request_transformer.go; see UPSTREAM.md.
package antigravity

import (
	"codearts2api/internal/googlewire"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/http"
	"strings"
)

func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "antigravity", Name: "Antigravity", Scopes: []string{"global"}, Login: true, Available: true}
}

func envelope(body []byte, model, project string) (json.RawMessage, error) {
	// The same native Gemini content format is used on this independently
	// authenticated endpoint. No hidden model replacement or identity prompt.
	native, err := googlewire.Encode(body, model)
	if err != nil {
		return nil, err
	}
	var request map[string]json.RawMessage
	if err = json.Unmarshal(native, &request); err != nil {
		return nil, err
	}
	// Antigravity accepts a numeric thinking budget (including Claude), not
	// Gemini CLI's thinkingLevel. Respect an explicit smaller output ceiling.
	var input map[string]json.RawMessage
	_ = json.Unmarshal(body, &input)
	if raw := input["reasoning_effort"]; len(raw) > 0 && string(raw) != "null" {
		var effort string
		_ = json.Unmarshal(raw, &effort)
		budget := map[string]int{"minimal": 1024, "low": 1024, "medium": 8192, "high": 24576}[effort]
		var config map[string]json.RawMessage
		_ = json.Unmarshal(request["generationConfig"], &config)
		if config == nil {
			config = map[string]json.RawMessage{}
		}
		var ceiling int
		_ = json.Unmarshal(config["maxOutputTokens"], &ceiling)
		if strings.HasPrefix(model, "claude-") && ceiling > 0 && ceiling <= budget {
			if ceiling <= 1024 {
				return nil, providerhttp.Invalid("Antigravity", "Claude 推理预算需要 max_tokens 大于 1024")
			}
			budget = ceiling - 1
		}
		config["thinkingConfig"], _ = json.Marshal(map[string]any{"includeThoughts": true, "thinkingBudget": budget})
		request["generationConfig"], _ = json.Marshal(config)
	}
	if len(request["toolConfig"]) == 0 {
		request["toolConfig"] = json.RawMessage(`{"functionCallingConfig":{"mode":"VALIDATED"}}`)
	}
	// Stable initial-user content, not a process-wide cache or persistent session.
	var contents []struct {
		Role  string          `json:"role"`
		Parts json.RawMessage `json:"parts"`
	}
	_ = json.Unmarshal(request["contents"], &contents)
	for _, content := range contents {
		if content.Role == "user" {
			sum := sha256.Sum256(content.Parts)
			request["sessionId"], _ = json.Marshal(hex.EncodeToString(sum[:16]))
			break
		}
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"model": model, "project": project, "request": request, "userAgent": "antigravity", "requestType": "agent", "requestId": "agent-" + hex.EncodeToString(id[:])})
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	var a credentials
	_ = json.Unmarshal(b, &a)
	if !providerhttp.ModelID(r.Model) {
		return providerhttp.Invalid("Antigravity", "型号无效")
	}
	body, err := envelope(r.Body, r.Model, a.Project)
	if err != nil {
		return err
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "Antigravity", http.MethodPost, c.BaseURL+"/v1internal:streamGenerateContent?alt=sse", http.Header{"Authorization": {"Bearer " + a.Access}, "User-Agent": {UserAgent}}, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return providerhttp.FailureResponse("Antigravity", resp)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken("Antigravity")
	}
	return googlewire.Decode(ctx, resp.Body, "Antigravity", r.Model, emit)
}
