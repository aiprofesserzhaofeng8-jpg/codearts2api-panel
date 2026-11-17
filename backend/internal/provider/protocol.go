// SPDX-License-Identifier: LGPL-3.0-only
// Protocol selection adapted from Sub2API upstream_protocol_routing.go,
// revision ba81c7fd758e425d67f856d601147ef0c5263d3f. See protocol-UPSTREAM.md.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

type Protocol string

const (
	ChatProtocol      Protocol = "chat_completions"
	ResponsesProtocol Protocol = "responses"
	MessagesProtocol  Protocol = "anthropic"
	AdaptiveProtocol  Protocol = "adaptive"
)

// ProtocolRoute is a projection of an adapter's existing profile, never an
// account selector or a second provider registry. Empty Setting retains Legacy.
type ProtocolRoute struct {
	Model     string
	Rules     []ProtocolRule
	Setting   Protocol
	Legacy    Protocol
	Supported []Protocol
	ByModel   bool
	Preferred Protocol
	Catalog   []Protocol
}

func (r ProtocolRoute) Resolve(inbound Protocol) (Protocol, error) {
	setting := r.Setting
	if setting == "" {
		setting = r.Legacy
	}
	if setting != AdaptiveProtocol {
		if !slices.Contains(r.Supported, setting) {
			return "", fmt.Errorf("账号模式不支持协议 %q", setting)
		}
		if r.ByModel && len(r.Catalog) > 0 && !slices.Contains(r.Catalog, setting) {
			return "", fmt.Errorf("模型目录不支持所选协议 %q", setting)
		}
		return setting, nil
	}
	if !r.ByModel {
		if slices.Contains(r.Supported, inbound) {
			return inbound, nil
		}
		if slices.Contains(r.Supported, ChatProtocol) {
			return ChatProtocol, nil
		}
		return "", fmt.Errorf("账号模式没有可转换的 Chat 端点")
	}
	if explicit := matchProtocolRules(r.Model, r.Rules); len(explicit) > 0 {
		if slices.Contains(explicit, inbound) {
			return inbound, nil
		}
		return explicit[0], nil
	}
	set := make([]Protocol, 0, len(r.Catalog))
	for _, p := range r.Catalog {
		if slices.Contains(r.Supported, p) && !slices.Contains(set, p) {
			set = append(set, p)
		}
	}
	if len(set) == 0 {
		if r.Rules != nil {
			set = []Protocol{ChatProtocol}
		} else {
			set = []Protocol{r.Preferred}
		}
	}
	if slices.Contains(set, inbound) {
		return inbound, nil
	}
	if slices.Contains(set, r.Preferred) {
		return r.Preferred, nil
	}
	return set[0], nil
}

// NativeStreaming preserves JSON events in their original protocol. It borrows
// credentials; admission, refresh, policies, accounting and retry stay outside.
type NativeStreaming interface {
	ResolveProtocol(json.RawMessage, Model, Protocol) (Protocol, error)
	StreamNative(context.Context, json.RawMessage, Request, func([]byte) error) error
}

// EndpointProtocols extracts only known endpoint kinds; catalog URLs never
// become executable addresses. Ordering is retained from supported_endpoints.
func EndpointProtocols(endpoints []string) []Protocol {
	var out []Protocol
	for _, endpoint := range endpoints {
		var p Protocol
		switch {
		case endpoint == "/chat/completions" || endpoint == "/v1/chat/completions" || endpoint == "/provider/v1/chat/completions", endpoint == "chat-completions", endpoint == string(ChatProtocol):
			p = ChatProtocol
		case endpoint == "/responses" || endpoint == "/v1/responses" || endpoint == "/provider/v1/responses", endpoint == string(ResponsesProtocol):
			p = ResponsesProtocol
		case endpoint == "/messages" || endpoint == "/v1/messages" || endpoint == "/provider/v1/messages", endpoint == "messages", endpoint == string(MessagesProtocol):
			p = MessagesProtocol
		}
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// ProtocolSettings is management-only; no secrets are returned by this view.
type ProtocolSettings interface {
	ProtocolOptions(json.RawMessage) (Protocol, []Protocol)
	SetProtocol(json.RawMessage, Protocol) (json.RawMessage, error)
}

func ChangeProtocol(raw json.RawMessage, p Protocol, allowed []Protocol, normalize func(json.RawMessage) (json.RawMessage, Identity, error)) (json.RawMessage, error) {
	if !slices.Contains(allowed, p) {
		return nil, fmt.Errorf("此账号不支持所选协议")
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, fmt.Errorf("账号配置无效")
	}
	obj["api_protocol"], _ = json.Marshal(p)
	updated, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	result, _, err := normalize(updated)
	return result, err
}
