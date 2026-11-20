package opencode

import (
	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
	"context"
	"encoding/json"
)

func (c *Client) ResolveProtocol(raw json.RawMessage, m provider.Model, inbound provider.Protocol) (provider.Protocol, error) {
	var a credentials
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", err
	}
	id := m.UpstreamID
	if id == "" {
		_, _, id = provider.ParseModel(m.ID)
	}
	preferred, ok := modelProtocol(a.Scope, id)
	if !ok {
		return "", providerhttp.Invalid("OpenCode", "暂不支持该型号的原生协议")
	}
	ps := provider.EndpointProtocols([]string{string(preferred)})
	// Omitted setting preserves this project's previous model-native route.
	legacy := ps[0]
	return (provider.ProtocolRoute{Model: id, Rules: a.Rules, Setting: a.Protocol, Legacy: legacy, ByModel: true, Preferred: legacy, Catalog: m.SupportedProtocols, Supported: []provider.Protocol{provider.ChatProtocol, provider.MessagesProtocol, provider.ResponsesProtocol}}).Resolve(inbound)
}
func (c *Client) StreamNative(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	return c.streamProtocol(ctx, raw, r, true, emit)
}

func (c *Client) ProtocolOptions(raw json.RawMessage) (provider.Protocol, []provider.Protocol) {
	var a credentials
	_ = json.Unmarshal(raw, &a)
	return a.Protocol, []provider.Protocol{provider.AdaptiveProtocol, provider.ChatProtocol, provider.ResponsesProtocol, provider.MessagesProtocol}
}
func (c *Client) SetProtocol(raw json.RawMessage, p provider.Protocol) (json.RawMessage, error) {
	_, options := c.ProtocolOptions(raw)
	return provider.ChangeProtocol(raw, p, options, c.Normalize)
}
