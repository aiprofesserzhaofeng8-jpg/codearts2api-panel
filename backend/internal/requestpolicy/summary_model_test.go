package requestpolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSummaryModelPreservesBusinessDataAndCallerCeiling(t *testing.T) {
	body := []byte(`{"model":"original","max_completion_tokens":64,"thinking":{"type":"enabled"},"reasoning_effort":"high","metadata":{"id":9007199254740993,"thinking":"business-data"},"messages":[{"role":"assistant","tool_calls":[{"function":{"arguments":"{\"id\":9007199254740993}"}}],"reasoning_content":"keep"},{"role":"tool","tool_call_id":"call","content":"keep"}],"future":{"enabled":false}}`)
	out, err := PrepareSummaryModel(body, "backup", SummaryOptions{MaxTokens: 1024}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(body, &before)
	_ = json.Unmarshal(out, &after)
	for _, field := range []string{"metadata", "future"} {
		if string(before[field]) != string(after[field]) {
			t.Fatal("business field changed", field, string(after[field]))
		}
	}
	for _, text := range []string{`"model":"backup"`, `"max_tokens":64`, `9007199254740993`, `"reasoning_content":"keep"`, `"tool_call_id":"call"`, SummaryRetryGuidance} {
		if !strings.Contains(string(out), text) {
			t.Fatal("request lost required content", text)
		}
	}
	if after["thinking"] != nil || after["reasoning_effort"] != nil || after["max_completion_tokens"] != nil {
		t.Fatal("original model capability controls leaked to replacement")
	}
}
