package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditMalformedToolArgumentsNotReplacedWithEmptyObject(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"tool_calls":[{"id":"a","function":{"name":"run","arguments":"{broken"}}]},"finish_reason":"tool_calls"}]}`)
	b, _ := json.Marshal(FromOpenAI(raw, "m").Payload)
	if strings.Contains(string(b), `"input":{}`) {
		t.Fatal("malformed tool arguments silently became an empty object")
	}
}

func TestAuditAggregateDoesNotDiscardBrokenFrames(t *testing.T) {
	valid := []byte(`{"choices":[{"index":0,"delta":{"content":"part"},"finish_reason":"stop"}]}`)
	for _, bad := range []string{`not-json`, `{"error":{"message":"fixture"}}`} {
		if _, err := AggregateSSE([][]byte{valid, []byte(bad)}); err == nil {
			t.Fatal("invalid/error chunk silently ignored")
		}
	}
}

func TestAuditUnknownMessageContentIsRejected(t *testing.T) {
	for _, req := range []*Request{
		{Messages: []Message{{Role: "user", Content: json.RawMessage(`[{"type":"future_media","payload":"important"}]`)}}},
		{System: json.RawMessage(`{"instructions":"do not lose"}`), Messages: []Message{{Role: "user", Content: json.RawMessage(`"hello"`)}}},
	} {
		if _, err := ToOpenAI(req, "m"); err == nil {
			t.Fatal("unrepresentable content silently removed")
		}
	}
}

func TestAuditParallelToolMediaAcrossMessages(t *testing.T) {
	req := &Request{Messages: []Message{
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"a","name":"read","input":{}},{"type":"tool_use","id":"b","name":"read","input":{}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"a","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"b","content":"done"}]`)},
	}}
	out, err := ToOpenAI(req, "m")
	if err != nil {
		t.Fatal(err)
	}
	msgs := out["messages"].([]map[string]any)
	if len(msgs) != 4 || msgs[1]["role"] != "tool" || msgs[2]["role"] != "tool" || msgs[3]["role"] != "user" {
		t.Fatal("lifted image interrupted parallel tool result batch", msgs)
	}
}
