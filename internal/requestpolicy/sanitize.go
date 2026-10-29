// Fixed client-template rewrites adapted from workbuddy2api-panel (MIT;
// LICENSE.workbuddy). Only system/developer text is eligible. These are
// compatibility transforms, not a general secret scrubber.
package requestpolicy

import (
	"encoding/json"
	"regexp"
	"strings"
)

// A complete standalone billing header is recognizable client metadata. Do not
// strip bare cc_* assignments, quoted header names, or arbitrary text fragments.
var billingHeaderLine = regexp.MustCompile(`(?im)^[\t ]*x-anthropic-billing-header:[^\r\n]*(?:\r?\n|$)`)
var clientTemplateRewrites = [][2]string{
	{"You are Claude Code, Anthropic's official CLI for Claude", "You are Claude Code, Anthropic's official CLI tool for Claude"},
	{"You are a coding agent running in the Codex CLI, a terminal-based coding assistant.", "You are a coding agent running in the Codex CLI tool, a terminal-based coding assistant."},
}

func sanitizeText(text string) string {
	for _, rw := range clientTemplateRewrites {
		text = strings.ReplaceAll(text, rw[0], rw[1])
	}
	if billingHeaderLine.MatchString(text) {
		text = billingHeaderLine.ReplaceAllString(text, "")
	}
	// Preserve whitespace and all business identifiers, including the string 11128.
	return text
}

// Unknown content shapes are preserved. Text-block metadata and numbers stay
// RawMessage so a cleaning edit cannot round integers or drop extension fields.
func sanitizeSystemContent(raw json.RawMessage) (json.RawMessage, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		clean := sanitizeText(text)
		if clean == text {
			return raw, false
		}
		result, _ := json.Marshal(clean)
		return result, true
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return raw, false
	}
	changed := false
	for i, part := range parts {
		var block map[string]json.RawMessage
		if json.Unmarshal(part, &block) != nil || block == nil {
			continue
		}
		var kind string
		if json.Unmarshal(block["type"], &kind) != nil || (kind != "text" && kind != "input_text" && kind != "output_text") {
			continue
		}
		if json.Unmarshal(block["text"], &text) != nil {
			continue
		}
		clean := sanitizeText(text)
		if clean == text {
			continue
		}
		block["text"], _ = json.Marshal(clean)
		parts[i], _ = json.Marshal(block)
		changed = true
	}
	if !changed {
		return raw, false
	}
	result, _ := json.Marshal(parts)
	return result, true
}
