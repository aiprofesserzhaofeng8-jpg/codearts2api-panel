package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFromOpenAIPreservesToolArgumentNumbers(t *testing.T) {
	for _, input := range []string{`{"id":9007199254740993,"nested":[18446744073709551615,1.234567890123456789]}`, `{}`, `null`} {
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{"id": "call_1", "function": map[string]any{"name": "lookup", "arguments": input}}}}}}})
		encoded, err := json.Marshal(FromOpenAI(raw, "m").Payload)
		if err != nil || !strings.Contains(string(encoded), `"input":`+input) {
			t.Fatalf("tool argument JSON changed: %s err=%v", encoded, err)
		}
	}
}
