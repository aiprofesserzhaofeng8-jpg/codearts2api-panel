// SPDX-License-Identifier: LGPL-3.0-only
// Cases adapted from Sub2API cn_providers_test.go / upstream_protocol_routing_test.go.
package provider

import "testing"

func TestProtocolRoutingDefaultsAndAdaptive(t *testing.T) {
	all := []Protocol{ChatProtocol, ResponsesProtocol, MessagesProtocol}
	for _, tc := range []struct {
		name     string
		r        ProtocolRoute
		in, want Protocol
	}{
		{"legacy Kimi", ProtocolRoute{Legacy: ChatProtocol, Supported: all}, MessagesProtocol, ChatProtocol},
		{"adaptive native", ProtocolRoute{Setting: AdaptiveProtocol, Supported: all}, MessagesProtocol, MessagesProtocol},
		{"Zhipu fallback", ProtocolRoute{Setting: AdaptiveProtocol, Supported: []Protocol{ChatProtocol, MessagesProtocol}}, ResponsesProtocol, ChatProtocol},
		{"pinned", ProtocolRoute{Setting: MessagesProtocol, Supported: all}, ResponsesProtocol, MessagesProtocol},
		{"catalog native", ProtocolRoute{Setting: AdaptiveProtocol, ByModel: true, Preferred: MessagesProtocol, Catalog: all, Supported: all}, ResponsesProtocol, ResponsesProtocol},
		{"catalog preferred", ProtocolRoute{Setting: AdaptiveProtocol, ByModel: true, Preferred: MessagesProtocol, Catalog: []Protocol{ChatProtocol, MessagesProtocol}, Supported: all}, ResponsesProtocol, MessagesProtocol},
		{"catalog overrides builtin", ProtocolRoute{Setting: AdaptiveProtocol, ByModel: true, Preferred: MessagesProtocol, Catalog: []Protocol{ChatProtocol}, Supported: all}, ResponsesProtocol, ChatProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.r.Resolve(tc.in)
			if err != nil || got != tc.want {
				t.Fatalf("got %s, %v; want %s", got, err, tc.want)
			}
		})
	}
	if _, err := (ProtocolRoute{Setting: ResponsesProtocol, Supported: []Protocol{ChatProtocol, MessagesProtocol}}).Resolve(ChatProtocol); err == nil {
		t.Fatal("accepted unsupported fixed protocol")
	}
	if got := EndpointProtocols([]string{"https://evil.example/chat/completions", "/v1/systemone"}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestExplicitModelProtocolRulesPrecedeCatalog(t *testing.T) {
	r := ProtocolRoute{Setting: AdaptiveProtocol, ByModel: true, Model: "claude-test", Preferred: MessagesProtocol, Supported: []Protocol{ChatProtocol, MessagesProtocol, ResponsesProtocol}, Catalog: []Protocol{MessagesProtocol}, Rules: []ProtocolRule{{Pattern: "claude-*", Protocol: ChatProtocol, Protocols: []Protocol{ResponsesProtocol}}}}
	if got, err := r.Resolve(ResponsesProtocol); err != nil || got != ResponsesProtocol {
		t.Fatal(got, err)
	}
	r.Model = "unmatched"
	r.Catalog = nil
	if got, err := r.Resolve(MessagesProtocol); err != nil || got != ChatProtocol {
		t.Fatal(got, err)
	}
}
