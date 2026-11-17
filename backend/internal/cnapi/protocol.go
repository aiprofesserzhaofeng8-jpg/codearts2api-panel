// SPDX-License-Identifier: LGPL-3.0-only
// Profile paths and routing adapted from Sub2API provider_profile.go and
// domain_constants.go at ba81c7fd758e425d67f856d601147ef0c5263d3f.
package cnapi

import (
	"codearts2api/internal/chatwire"
	"codearts2api/internal/messagewire"
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"codearts2api/internal/responsewire"
	"context"
	"encoding/json"
	"strings"
)

func (c *Client) protocols() []provider.Protocol {
	switch c.ID {
	case provider.DeepSeek, provider.Kimi, provider.MiniMax:
		return []provider.Protocol{provider.ChatProtocol, provider.ResponsesProtocol, provider.MessagesProtocol}
	case provider.Zhipu:
		return []provider.Protocol{provider.ChatProtocol, provider.MessagesProtocol}
	}
	return []provider.Protocol{provider.ChatProtocol}
}
func (c *Client) ResolveProtocol(raw json.RawMessage, _ provider.Model, inbound provider.Protocol) (provider.Protocol, error) {
	if p := c.profile(raw); p != c {
		return p.ResolveProtocol(raw, provider.Model{}, inbound)
	}
	var a credentials
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", err
	}
	if inbound == "" {
		inbound = provider.ChatProtocol
	}
	return (provider.ProtocolRoute{Setting: a.Protocol, Legacy: provider.ChatProtocol, Supported: c.protocols()}).Resolve(inbound)
}
func (c *Client) protocolEndpoint(p provider.Protocol) string {
	switch p {
	case provider.ChatProtocol:
		return c.Base + "/chat/completions"
	case provider.ResponsesProtocol:
		base := c.Base
		if c.ID == provider.DeepSeek {
			base = strings.TrimSuffix(base, "/v1")
		}
		return base + "/responses"
	case provider.MessagesProtocol:
		base := strings.TrimSuffix(c.Base, "/v1")
		if c.ID == provider.Zhipu {
			if i := strings.Index(base, "/api/"); i >= 0 {
				base = base[:i] + "/api"
			}
		}
		if c.ID != provider.Kimi || c.mode() != "coding" {
			base += "/anthropic"
		}
		return base + "/v1/messages"
	}
	return ""
}
func (c *Client) StreamNative(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	if p := c.profile(raw); p != c {
		return p.StreamNative(ctx, raw, r, emit)
	}
	target, err := c.ResolveProtocol(raw, provider.Model{}, r.Protocol)
	if err != nil {
		return err
	}
	if target != r.Protocol {
		return providerhttp.Invalid(c.Name, "请求协议与账号配置不一致")
	}
	body, err := providerhttp.NativeBody(r, false)
	if err != nil {
		return err
	}
	return c.protocolStream(ctx, raw, r, target, body, true, emit)
}
func (c *Client) protocolStream(ctx context.Context, raw json.RawMessage, r provider.Request, p provider.Protocol, body any, native bool, emit func([]byte) error) error {
	resp, err := c.request(ctx, raw, "POST", c.protocolEndpoint(p), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return c.failure(resp)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return providerhttp.Broken(c.Name)
	}
	if native {
		return providerhttp.NativeSSE(ctx, resp.Body, p, c.Name, emit)
	}
	switch p {
	case provider.MessagesProtocol:
		return messagewire.Decode(ctx, resp.Body, r.Model, c.Name, emit)
	case provider.ResponsesProtocol:
		return responsewire.Decode(ctx, resp.Body, r.Model, c.Name, emit)
	default:
		return chatwire.Decode(ctx, resp.Body, c.Name, emit)
	}
}

func (c *Client) ProtocolOptions(raw json.RawMessage) (provider.Protocol, []provider.Protocol) {
	if p := c.profile(raw); p != c {
		return p.ProtocolOptions(raw)
	}
	var a credentials
	_ = json.Unmarshal(raw, &a)
	if len(c.protocols()) < 2 {
		return "", nil
	}
	p := a.Protocol
	if p == "" {
		p = provider.ChatProtocol
	}
	return p, append([]provider.Protocol{provider.AdaptiveProtocol}, c.protocols()...)
}
func (c *Client) SetProtocol(raw json.RawMessage, p provider.Protocol) (json.RawMessage, error) {
	_, options := c.ProtocolOptions(raw)
	return provider.ChangeProtocol(raw, p, options, c.Normalize)
}
