package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicThinkingPreferences(t *testing.T) {
	for _, tc := range []struct {
		name, fields, wantError string
		disabled                bool
	}{
		{name: "omitted"},
		{name: "null", fields: `,"thinking":null,"output_config":null`},
		{name: "disabled", fields: `,"thinking":{"type":"disabled"}`, disabled: true},
		{name: "enabled", fields: `,"thinking":{"type":"enabled"}`, wantError: "thinking.type"},
		{name: "adaptive_cli", fields: `,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}`, wantError: "output_config.effort"},
		{name: "adaptive", fields: `,"thinking":{"type":"adaptive"}`, wantError: "thinking.type"},
		{name: "enabled_budget_cli", fields: `,"thinking":{"type":"enabled","budget_tokens":4096}`, wantError: "budget_tokens"},
		{name: "disabled_budget", fields: `,"thinking":{"type":"disabled","budget_tokens":0}`, wantError: "budget_tokens"},
		{name: "effort", fields: `,"output_config":{"effort":"low"}`, wantError: "output_config.effort"},
		{name: "disabled_and_effort", fields: `,"thinking":{"type":"disabled"},"output_config":{"effort":"high"}`, wantError: "output_config.effort"},
		{name: "wrong_thinking_type", fields: `,"thinking":true`, wantError: "thinking 必须为对象"},
		{name: "missing_type", fields: `,"thinking":{}`, wantError: "thinking.type"},
		{name: "numeric_type", fields: `,"thinking":{"type":1}`, wantError: "thinking.type"},
		{name: "unknown_options", fields: `,"thinking":{"type":"disabled","display":"omitted"}`, wantError: "思考选项"},
		{name: "wrong_config", fields: `,"output_config":[]`, wantError: "output_config 必须为对象"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req Request
			err := json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"11128 cc_version=42"}],"max_tokens":64`+tc.fields+`}`), &req)
			if err != nil {
				t.Fatal(err)
			}
			out, err := ToOpenAI(&req, "deepseek-v4.1-flash")
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || out != nil {
					t.Fatalf("want %q, got output=%v error=%v", tc.wantError, out, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := `{"model":"deepseek-v4.1-flash","stream":false,"max_tokens":64,"messages":[{"role":"user","content":"11128 cc_version=42"}]`
			if tc.disabled {
				want += `,"thinking":{"type":"disabled"}`
			}
			jsonEq(t, out, want+`}`)
		})
	}
}
