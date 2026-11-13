package protocol

import (
	"encoding/json"
	"testing"
)

func TestMessagesHistoryNativeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, block     string
		native, invalid bool
	}{
		{"plain", `{"type":"thinking","thinking":"reason"}`, false, false},
		{"empty_signature", `{"type":"thinking","thinking":"reason","signature":""}`, false, false},
		{"null_signature", `{"type":"thinking","thinking":"reason","signature":null}`, false, false},
		{"signed", `{"type":"thinking","thinking":"reason","signature":"opaque"}`, true, true},
		{"invalid_signature", `{"type":"thinking","thinking":"reason","signature":42}`, true, true},
		{"redacted", `{"type":"redacted_thinking","data":"opaque"}`, true, true},
		{"invalid_text", `{"type":"thinking","thinking":42}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := Request{Messages: []Message{{Role: "assistant", Content: json.RawMessage("[" + tc.block + "]")}}}
			if RequiresNativeMessages(&req) != tc.native {
				t.Fatal("wrong native route classification")
			}
			out, err := ToOpenAI(&req, "model")
			if (err != nil) != tc.invalid {
				t.Fatal(out, err)
			}
			if !tc.invalid {
				jsonEq(t, out["messages"], `[{"role":"assistant","content":"","reasoning_content":"reason"}]`)
			}
			req.Messages[0].Role = "user"
			if _, err := ToOpenAI(&req, "model"); err == nil {
				t.Fatal("user thinking accepted")
			}
		})
	}
	for _, control := range []string{`{"thinking":{"type":"enabled","budget_tokens":1024}}`, `{"thinking":{"type":"adaptive"}}`, `{"output_config":{"effort":"high"}}`} {
		var req Request
		_ = json.Unmarshal([]byte(control), &req)
		if !RequiresNativeMessages(&req) {
			t.Fatal("native control escaped", control)
		}
	}
}

func TestMessagesHistoryPreservesToolsNumbersAndImages(t *testing.T) {
	var req Request
	raw := `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"first\n"},{"type":"thinking","thinking":"second"},{"type":"text","text":"visible"},{"type":"tool_use","id":"call-A","name":"lookup","input":{"n":9007199254740993}},{"type":"tool_use","id":"call-B","name":"read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-A","content":"RESULT-A"},{"type":"tool_result","tool_use_id":"call-B","content":[{"type":"text","text":"RESULT-B"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"fixture"}}]}]}]}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	out, err := ToOpenAI(&req, "model")
	if err != nil {
		t.Fatal(err)
	}
	jsonEq(t, out["messages"], `[{"role":"assistant","content":"visible","reasoning_content":"first\nsecond","tool_calls":[{"id":"call-A","type":"function","function":{"name":"lookup","arguments":"{\"n\":9007199254740993}"}},{"id":"call-B","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-A","content":"RESULT-A"},{"role":"tool","tool_call_id":"call-B","content":"RESULT-B"},{"role":"user","content":[{"type":"text","text":"Images returned by tool call-B:"},{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}]`)
}

func TestSignedMessagesHistoryPreservedNatively(t *testing.T) {
	blocks := json.RawMessage(`[{"type":"thinking","thinking":"reason","signature":"signed"},{"type":"redacted_thinking","data":"encrypted"}]`)
	req := Request{Messages: []Message{{Role: "assistant", Content: blocks}}}
	out, err := ToOpenAINativeMessages(&req, "claude")
	if err != nil {
		t.Fatal(err)
	}
	messages := out["messages"].([]map[string]any)
	jsonEq(t, messages[0]["anthropic_thinking_blocks"], string(blocks))
}
