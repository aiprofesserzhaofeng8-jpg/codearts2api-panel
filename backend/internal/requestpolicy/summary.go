package requestpolicy

import (
	"encoding/json"
	"strings"

	"codearts2api/internal/protocol"
)

const SummaryGuidance = "Produce a concise checkpoint, not an exhaustive transcript. Keep the summary under 1800 words. Preserve required headings, user goal, active constraints, essential paths/IDs, completed work, blockers and concrete next steps. Deduplicate repeated attempts. Do not reproduce source code, long logs or full tool output. Finish every required section within this limit."
const SummaryRetryGuidance = "The previous summary exceeded the output budget. This is a replacement attempt, not a continuation. Finish a much shorter checkpoint under 900 words. Preserve only actionable essentials and required headings. Do not quote the transcript."

// SummaryOptions is a per-attempt snapshot, not a persisted model policy or a
// declaration of model capability. Selection, retries and billing stay outside.
type SummaryOptions struct {
	MaxTokens       int
	DisableThinking bool
}

// IsSummaryRequest preserves the existing explicit/Pi detection rules. Tools
// prevent summary classification even when the explicit header is present.
func IsSummaryRequest(body []byte, explicit bool) bool {
	var req struct {
		Tools    []json.RawMessage `json:"tools"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Tools) > 0 {
		return false
	}
	if explicit {
		return true
	}
	for _, m := range req.Messages {
		var text string
		_ = json.Unmarshal(m.Content, &text)
		if (m.Role == "system" || m.Role == "developer") && strings.HasPrefix(text, "You are a context summarization assistant.") {
			return true
		}
	}
	return false
}

// PrepareSummary adds bounded checkpoint guidance and stream usage options.
// It keeps raw business fields and the caller's lower positive output ceiling.
func PrepareSummary(body []byte, options SummaryOptions, retry bool) ([]byte, error) {
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(req["messages"], &messages); err != nil {
		return nil, err
	}
	guidance := SummaryGuidance
	if retry {
		guidance = SummaryRetryGuidance
	}
	added := false
	for _, m := range messages {
		var role, text string
		_ = json.Unmarshal(m["role"], &role)
		_ = json.Unmarshal(m["content"], &text)
		if role == "system" || role == "developer" {
			if !strings.Contains(text, guidance) {
				text += "\n\n" + guidance
			}
			m["content"], _ = json.Marshal(text)
			added = true
			break
		}
	}
	if !added {
		role, _ := json.Marshal("system")
		text, _ := json.Marshal(guidance)
		messages = append([]map[string]json.RawMessage{{"role": role, "content": text}}, messages...)
	}
	if options.DisableThinking {
		req["thinking"] = json.RawMessage(`{"type":"disabled"}`)
		delete(req, "reasoning_effort")
	}
	cap := options.MaxTokens
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		var n int
		if json.Unmarshal(req[field], &n) == nil && n > 0 {
			cap = min(cap, n)
		}
	}
	req["max_tokens"], _ = json.Marshal(cap)
	delete(req, "max_completion_tokens")
	req["messages"], _ = json.Marshal(messages)
	req["stream"] = json.RawMessage("true")
	raw, _ := json.Marshal(req)
	return protocol.RequestStreamUsage(raw)
}
