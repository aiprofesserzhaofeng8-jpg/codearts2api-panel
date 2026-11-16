// SPDX-License-Identifier: LGPL-3.0-only
// Fixed Cline Chat endpoint adapted from Sub2API cline.go; see UPSTREAM.md.
package cline

import (
	"codearts2api/internal/chatwire"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

func (c *Client) Describe() provider.Descriptor {
	return provider.Descriptor{ID: "cline", Name: "Cline", Scopes: []string{"global"}, Available: true}
}
func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	b, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	var a credentials
	json.Unmarshal(b, &a)
	if !validModel(r.Model) {
		return providerhttp.Invalid("Cline", "型号无效")
	}
	body, err := chatwire.EncodeQualified(r, "Cline")
	if err != nil {
		return err
	}
	resp, err := providerhttp.Do(ctx, c.HTTP, "Cline", "POST", c.Origin+"/api/v1/chat/completions", http.Header{"Authorization": {"Bearer " + a.Key}, "X-Title": {"CodeArts2API"}}, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return classifyFailure(b, providerhttp.FailureResponse("Cline", resp))
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if typ != "text/event-stream" {
		return providerhttp.Broken("Cline")
	}
	return chatwire.DecodeWithError(ctx, resp.Body, "Cline", func(b []byte) error { return classifyFailure(b, providerhttp.Broken("Cline")) }, func(b []byte) error {
		normalized, err := normalizeReasoning(b)
		if err != nil {
			return err
		}
		return emit(normalized)
	})
}

// Authentication is checked independently of public catalogs and free inference.
func (c *Client) Verify(ctx context.Context, raw json.RawMessage) (provider.Identity, error) {
	_, id, err := c.Normalize(raw)
	if err != nil {
		return id, err
	}
	me, err := c.account(ctx, raw, "/api/v1/users/me")
	if err != nil {
		return id, err
	}
	if providerhttp.Field(me, "id") == "" {
		return id, providerhttp.Broken("Cline")
	}
	return id, nil
}

var _ provider.Adapter = (*Client)(nil)

// Map Cline's plain reasoning alias to the existing canonical field. Opaque
// reasoning_details are retained in Chat chunks, never turned into text/signatures.
func normalizeReasoning(b []byte) ([]byte, error) {
	var chunk map[string]json.RawMessage
	if json.Unmarshal(b, &chunk) != nil {
		return nil, providerhttp.Broken("Cline")
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(chunk["choices"], &choices) != nil {
		return nil, providerhttp.Broken("Cline")
	}
	changed := false
	for _, choice := range choices {
		var finish string
		_ = json.Unmarshal(choice["finish_reason"], &finish)
		if finish == "error" {
			return nil, providerhttp.Broken("Cline")
		}
		var delta map[string]json.RawMessage
		if len(choice["delta"]) == 0 || string(choice["delta"]) == "null" {
			continue
		}
		if json.Unmarshal(choice["delta"], &delta) != nil {
			return nil, providerhttp.Broken("Cline")
		}
		if value, ok := delta["reasoning"]; ok && string(value) != "null" {
			var text string
			if json.Unmarshal(value, &text) != nil {
				return nil, providerhttp.Broken("Cline")
			}
			if current, exists := delta["reasoning_content"]; !exists || string(current) == "null" {
				delta["reasoning_content"] = value
				choice["delta"], _ = json.Marshal(delta)
				changed = true
			}
		}
	}
	if !changed {
		return b, nil
	}
	chunk["choices"], _ = json.Marshal(choices)
	return json.Marshal(chunk)
}
