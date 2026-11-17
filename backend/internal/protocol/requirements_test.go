package protocol

import (
	"testing"
)

func TestNativeCapabilityInspection(t *testing.T) {
	for _, tc := range []struct {
		p    string
		body string
		want Requirements
	}{
		{"anthropic", `{"max_tokens":100,"messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"image","source":{}}]}]}]}`, Requirements{true, true, 100}},
		{"responses", `{"max_output_tokens":200,"input":[{"type":"function_call","arguments":"{\"type\":\"image\"}"}]}`, Requirements{false, true, 200}},
		{"responses", `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"fixture"}]}]}`, Requirements{true, false, 0}},
		{"chat_completions", `{"messages":[{"role":"user","content":"image"}],"tools":[{"parameters":{"type":"image"}}]}`, Requirements{false, true, 0}},
	} {
		got, err := InspectRequirements([]byte(tc.body), tc.p)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %+v %v", tc.p, got, err)
		}
	}
}
