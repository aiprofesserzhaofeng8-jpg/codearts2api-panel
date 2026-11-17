package requestpolicy

import (
	"bytes"
	"codearts2api/internal/provider"
	"encoding/json"
	"strings"
	"testing"
)

func TestAppendPreservesClientInstructionsAndOpaquePayload(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    provider.Protocol
		body string
	}{
		{"chat", provider.ChatProtocol, `{"messages":[{"role":"system","content":"CLIENT"},{"role":"developer","content":"PROJECT"},{"role":"user","content":"QUESTION"}],"tools":[{"parameters":{"n":9007199254740993}}]}`},
		{"responses", provider.ResponsesProtocol, `{"instructions":"CLIENT","input":[{"role":"developer","content":"PROJECT"},{"type":"function_call","call_id":"call1","arguments":"{\"n\":9007199254740993}"}]}`},
		{"messages", provider.MessagesProtocol, `{"system":[{"type":"text","text":"CLIENT","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"PROJECT","signature":"signed"},{"type":"tool_use","id":"call1","input":{"n":9007199254740993}}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := []byte(tc.body)
			out, err := PrepareProtocolBody(original, tc.p, "append", "EXTRA", false)
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"CLIENT", "PROJECT", "9007199254740993", "EXTRA"} {
				if !bytes.Contains(out, []byte(text)) {
					t.Fatalf("lost %s: %s", text, out)
				}
			}
			if !json.Valid(out) || string(original) != tc.body || strings.Count(string(out), "EXTRA") != 1 {
				t.Fatal(string(out))
			}
			again, err := PrepareProtocolBody(original, tc.p, "append", "EXTRA", false)
			if err != nil || !bytes.Equal(out, again) {
				t.Fatal("retry from original changed request")
			}
			unchanged, err := PrepareProtocolBody(original, tc.p, "append", "", false)
			if err != nil || !bytes.Equal(unchanged, original) {
				t.Fatal("empty append used built-in prompt")
			}
			if tc.p == provider.MessagesProtocol && (!bytes.Contains(out, []byte("cache_control")) || !bytes.Contains(out, []byte("signed"))) {
				t.Fatal(string(out))
			}
		})
	}
}

func TestAppendRespectsDisabledTemplateCleaning(t *testing.T) {
	template := "You are a coding agent running in the Codex CLI, a terminal-based coding assistant."
	for _, p := range []provider.Protocol{provider.MessagesProtocol, provider.ResponsesProtocol} {
		request := map[string]any{"system": template, "instructions": template, "input": []any{map[string]string{"role": "developer", "content": template}}}
		body, _ := json.Marshal(request)
		out, err := PrepareProtocolBody(body, p, "append", "EXTRA", false)
		if err != nil || bytes.Contains(out, []byte("Codex CLI tool")) {
			t.Fatal(string(out), err)
		}
	}
}
