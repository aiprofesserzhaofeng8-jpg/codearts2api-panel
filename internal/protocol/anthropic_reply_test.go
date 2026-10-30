package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestFromOpenAIRefusalAndCacheUsage(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"reasoning_content":"reason","content":"prefix","refusal":"拒答正文","tool_calls":[{"id":"call_1","function":{"name":"lookup","arguments":"{\"id\":9007199254740993}"}}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":8}}}`)
	reply := FromOpenAI(raw, "m")
	encoded, err := json.Marshal(reply.Payload)
	if err != nil || !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("tool argument number lost: %s err=%v", encoded, err)
	}
	if reply.Text != "prefix拒答正文" || reply.Thinking != "reason" {
		t.Fatalf("text/refusal or reasoning lost: text=%q reasoning=%q", reply.Text, reply.Thinking)
	}
	jsonEq(t, reply.Payload["content"], `[{"type":"thinking","thinking":"reason"},{"type":"text","text":"prefix"},{"type":"text","text":"拒答正文"},{"type":"tool_use","id":"call_1","name":"lookup","input":{"id":9007199254740993}}]`)
	jsonEq(t, reply.Payload["usage"], `{"input_tokens":4,"cache_read_input_tokens":8,"output_tokens":2}`)
	if reply.Payload["stop_reason"] != "tool_use" {
		t.Fatal("refusal changed the tool finish reason")
	}
}

func TestFromOpenAIUsageCounts(t *testing.T) {
	for _, tc := range []struct{ name, usage, want string }{
		{"no_cache", `{"prompt_tokens":12,"completion_tokens":2}`, `{"input_tokens":12,"output_tokens":2}`},
		{"cache", `{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":8}}`, `{"input_tokens":4,"cache_read_input_tokens":8,"output_tokens":2}`},
		{"alias", `{"prompt_tokens":12,"completion_tokens":2,"prompt_cache_hit_tokens":8}`, `{"input_tokens":4,"cache_read_input_tokens":8,"output_tokens":2}`},
		{"prefer_details", `{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":8},"prompt_cache_hit_tokens":9}`, `{"input_tokens":4,"cache_read_input_tokens":8,"output_tokens":2}`},
		{"zero_details_alias", `{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":0},"prompt_cache_hit_tokens":8}`, `{"input_tokens":4,"cache_read_input_tokens":8,"output_tokens":2}`},
		{"explicit_zero", `{"prompt_tokens":0,"completion_tokens":0}`, `{"input_tokens":0,"output_tokens":0}`},
		{"all_cached", `{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":12}}`, `{"input_tokens":0,"cache_read_input_tokens":12,"output_tokens":2}`},
		{"reported_zero_cache", `{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":0}}`, `{"input_tokens":12,"cache_read_input_tokens":0,"output_tokens":2}`},
		{"negative_cache", `{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":-3}}`, `{"input_tokens":12,"cache_read_input_tokens":0,"output_tokens":2}`},
		{"oversized_cache", `{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":99}}`, `{"input_tokens":0,"cache_read_input_tokens":12,"output_tokens":2}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"choices":[{"message":{"content":"text"},"finish_reason":"stop"}],"usage":%s}`, tc.usage))
			jsonEq(t, FromOpenAI(raw, "m").Payload["usage"], tc.want)
		})
	}
	for _, usage := range []string{`null`, `{}`, `{"prompt_tokens":null,"completion_tokens":null}`} {
		raw := []byte(fmt.Sprintf(`{"choices":[{"message":{"refusal":"拒答"},"finish_reason":"stop"}],"usage":%s}`, usage))
		reply := FromOpenAI(raw, "m")
		want := fmt.Sprintf(`{"input_tokens":%d,"output_tokens":2}`, EstimatePromptLen(raw))
		jsonEq(t, reply.Payload["usage"], want)
		if reply.Text != "拒答" || reply.Payload["stop_reason"] != "end_turn" {
			t.Fatal("missing usage hid refusal or changed stop semantics")
		}
	}
}

func BenchmarkFromOpenAIReply(b *testing.B) {
	for _, tc := range []struct{ name, message, usage string }{
		{"text", `{"content":"normal text"}`, `{"prompt_tokens":12,"completion_tokens":4}`},
		{"cache", `{"content":"normal text"}`, `{"prompt_tokens":12,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":8}}`},
		{"tools", `{"content":"","tool_calls":[{"id":"call_1","function":{"name":"lookup","arguments":"{\"id\":9007199254740993}"}}]}`, `{"prompt_tokens":12,"completion_tokens":4}`},
	} {
		b.Run(tc.name, func(b *testing.B) {
			raw := []byte(fmt.Sprintf(`{"choices":[{"message":%s,"finish_reason":"stop"}],"usage":%s}`, tc.message, tc.usage))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := json.Marshal(FromOpenAI(raw, "fixture").Payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
