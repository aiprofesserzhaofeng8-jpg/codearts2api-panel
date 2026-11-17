// SPDX-License-Identifier: LGPL-3.0-only
// Adapted from Sub2API opencode_go.go at ba81c7fd758e425d67f856d601147ef0c5263d3f.
package provider

import (
	"fmt"
	"slices"
	"strings"
)

type ProtocolRule struct {
	Pattern   string     `json:"pattern"`
	Protocol  Protocol   `json:"protocol"`
	Protocols []Protocol `json:"protocols,omitempty"`
}

func ValidateProtocolRules(rules []ProtocolRule) error {
	if len(rules) > 64 {
		return fmt.Errorf("protocol_rules 最多 64 条")
	}
	for i := range rules {
		r := &rules[i]
		r.Pattern = strings.ToLower(strings.TrimSpace(r.Pattern))
		if r.Pattern == "" || len(r.Pattern) > 200 || strings.ContainsAny(r.Pattern, " \t\r\n") || strings.Count(r.Pattern, "*") > 1 || strings.Contains(r.Pattern, "*") && !strings.HasSuffix(r.Pattern, "*") {
			return fmt.Errorf("protocol_rules[%d] 的 pattern 仅支持精确名称或末尾 *", i)
		}
		if r.Protocol == "" && len(r.Protocols) > 0 {
			r.Protocol = r.Protocols[0]
		}
		for _, p := range append([]Protocol{r.Protocol}, r.Protocols...) {
			if p != ChatProtocol && p != ResponsesProtocol && p != MessagesProtocol {
				return fmt.Errorf("protocol_rules[%d] 的协议无效", i)
			}
		}
	}
	return nil
}
func matchProtocolRules(model string, rules []ProtocolRule) []Protocol {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, r := range rules {
		if r.Pattern == model || strings.HasSuffix(r.Pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(r.Pattern, "*")) {
			out := []Protocol{r.Protocol}
			for _, p := range r.Protocols {
				if !slices.Contains(out, p) {
					out = append(out, p)
				}
			}
			return out
		}
	}
	return nil
}
