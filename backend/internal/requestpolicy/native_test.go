package requestpolicy

import (
	"codearts2api/internal/provider"
	"strings"
	"testing"
)

func TestNativePromptPolicyPreservesOpaqueContent(t *testing.T) {
	for _, tc := range []struct {
		p     provider.Protocol
		body  string
		field string
	}{
		{provider.MessagesProtocol, `{"system":[{"type":"text","text":"old","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"assistant","content":[{"type":"tool_use","input":{"n":9007199254740993}}]}]}`, "system"},
		{provider.ResponsesProtocol, `{"instructions":"old","input":[{"role":"developer","content":"old"},{"type":"function_call","arguments":"{\"n\":9007199254740993}"}]}`, "instructions"},
	} {
		original := []byte(tc.body)
		got, err := PrepareProtocolBody(original, tc.p, "passthrough", "", false)
		if err != nil || string(got) != tc.body {
			t.Fatal(string(got), err)
		}
		got, err = PrepareProtocolBody(original, tc.p, "custom", "new", false)
		if err != nil || !strings.Contains(string(got), "9007199254740993") || strings.Contains(string(got), `"old"`) || !strings.Contains(string(got), `"`+tc.field+`":"new"`) {
			t.Fatal(string(got), err)
		}
	}
}
