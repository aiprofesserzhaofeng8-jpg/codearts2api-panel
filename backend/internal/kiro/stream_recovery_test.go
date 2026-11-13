package kiro

import (
	"bytes"
	"testing"
)

func TestEmptyFramesDoNotCommitAndDamagedReasoningFails(t *testing.T) {
	for name, stream := range map[string][]byte{
		"empty_text":          eventFrame("assistantResponseEvent", `{"content":""}`),
		"malformed_reasoning": append(eventFrame("assistantResponseEvent", `{"content":"before"}`), eventFrame("reasoningContentEvent", `{"text":42}`)...),
		"null_reasoning":      eventFrame("reasoningContentEvent", `{"text":null}`),
		"partial_header":      append(eventFrame("assistantResponseEvent", `{"content":"before"}`), 1, 2, 3),
	} {
		t.Run(name, func(t *testing.T) {
			var output [][]byte
			err := convertStream(t.Context(), bytes.NewReader(stream), "fixture", func(b []byte) error { output = append(output, b); return nil })
			if err == nil || bytes.Contains(bytes.Join(output, nil), []byte(`"finish_reason":"stop"`)) {
				t.Fatal("invalid stream completed", err)
			}
			if name == "empty_text" && len(output) != 0 {
				t.Fatal("empty response committed output")
			}
		})
	}
}
