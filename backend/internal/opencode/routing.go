// SPDX-License-Identifier: LGPL-3.0-only
// Adapted Sub2API DefaultOpenCode{Go,Zen}ProtocolRules; see UPSTREAM.md.
package opencode

import "strings"

type protocol string

const (
	chat      protocol = "chat-completions"
	responses protocol = "responses"
	messages  protocol = "messages"
)

// This table is a documented native protocol choice, not a model alias map.
// Current official catalog is checked against it before publishing sources.
func modelProtocol(scope, model string) (protocol, bool) {
	if scope != "zen" && scope != "go" {
		return "", false
	}
	lower := strings.ToLower(model)
	if strings.HasPrefix(lower, "gemini-") || strings.HasPrefix(lower, "jev-") {
		return "", false
	}
	for _, prefix := range []string{"gpt-", "grok-", "muse-spark-"} {
		if strings.HasPrefix(lower, prefix) {
			return responses, true
		}
	}
	if scope == "zen" && lower == "qwen3.8-max" {
		return chat, true
	}
	if strings.HasPrefix(lower, "claude-") || strings.HasPrefix(lower, "qwen") {
		return messages, true
	}
	// The pinned upstream's minimax-* Go rule predates M3.1 Flash's Chat API.
	if scope == "go" && strings.HasPrefix(lower, "minimax-") && !strings.HasPrefix(lower, "minimax-m3.1-flash") {
		return messages, true
	}
	return chat, true
}
func protocolPath(p protocol) string {
	switch p {
	case responses:
		return "/openai/v1/responses"
	case messages:
		return "/anthropic/v1/messages"
	case chat:
		return "/openai/v1/chat/completions"
	}
	return ""
}
