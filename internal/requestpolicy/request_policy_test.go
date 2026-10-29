package requestpolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestPolicyPreservesStructuredPayload(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"system","content":"old-system"},{"role":"developer","content":"old-developer"},{"role":"user","content":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."},{"type":"image_url","image_url":{"url":"https://example.test/11128.png"}}]},{"role":"assistant","content":null,"tool_calls":[{"id":"11128","type":"function","function":{"name":"write","arguments":"{\"body\":\"code 11128\",\"count\":11128,\"large\":9007199254740993}"}}]}],"seed":9007199254740993,"tools":[{"type":"function","function":{"name":"write","parameters":{"type":"object","properties":{"11128":{"type":"string"}}}}}]}`)
	exact, err := PrepareBody(raw, "passthrough", "", false)
	if err != nil || string(exact) != string(raw) {
		t.Fatal("disabled transformation must preserve body")
	}
	result, err := PrepareBody(raw, "custom", "CUSTOM", true)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	_ = json.Unmarshal(result, &got)
	if string(got["seed"]) != "9007199254740993" {
		t.Fatal("lost numeric precision")
	}
	var messages []map[string]json.RawMessage
	_ = json.Unmarshal(got["messages"], &messages)
	if len(messages) != 3 || string(messages[0]["content"]) != `"CUSTOM"` || strings.Contains(string(result), "old-system") || strings.Contains(string(result), "old-developer") {
		t.Fatal("system replacement failed")
	}
	if !strings.Contains(string(result), "CLI for Claude") || !strings.Contains(string(result), "https://example.test/11128.png") {
		t.Fatal("text and image boundaries")
	}
	var calls []struct {
		ID       string `json:"id"`
		Function struct {
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	_ = json.Unmarshal(messages[2]["tool_calls"], &calls)
	var args map[string]json.RawMessage
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if calls[0].ID != "11128" || string(args["count"]) != "11128" || string(args["large"]) != "9007199254740993" || string(args["body"]) != `"code 11128"` {
		t.Fatalf("tool args corrupted: %s", calls[0].Function.Arguments)
	}
	var before map[string]json.RawMessage
	_ = json.Unmarshal(raw, &before)
	if string(got["tools"]) != string(before["tools"]) {
		t.Fatal("tool schema changed")
	}
	clean, err := PrepareBody(raw, "custom", "", false)
	if err != nil || !strings.Contains(string(clean), "CLI for Claude") || strings.Contains(string(clean), "old-system") {
		t.Fatal("independent switches")
	}
}

func TestPassthroughDoesNotCleanBusinessData(t *testing.T) {
	raw := []byte(`{ "messages": [{"role":"system","content":"  11128  "},{"role":"user","content":"You are Claude Code, Anthropic's official CLI for Claude. cc_version=42; 11128"},{"role":"tool","content":"x-anthropic-billing-header: keep;","tool_call_id":"call-11128"},{"role":"assistant","content":null,"reasoning":"X-Anthropic-Billing-Header: keep;","tool_calls":[{"id":"call-11128","type":"function","function":{"name":"write","arguments":"{\"n\":9007199254740993,\"text\":\"cc_version=42; 11128\"}"}}]}], "seed":9007199254740993 }`)
	for _, enabled := range []bool{false, true} {
		out, err := PrepareBody(raw, "passthrough", "", enabled)
		if err != nil || string(out) != string(raw) {
			t.Fatalf("business data or no-op bytes changed: %v %s", err, out)
		}
	}
}

func TestSystemCleaningPreservesMetadataAndOtherRoles(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"system","content":"x-anthropic-billing-header: cc_version=1; cc_entrypoint=cli;\nKeep 11128 and cc_version=42; exactly.\n"},{"role":"developer","content":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude.","extra":9007199254740993},{"type":"image_url","image_url":{"url":"https://example.test/11128"}},{"type":"future","data":9007199254740993}]},{"role":"user","content":"You are Claude Code, Anthropic's official CLI for Claude. 11128"},{"role":"assistant","reasoning":"cc_version=42;","tool_calls":[{"id":"id","function":{"arguments":"{\"text\":\"11128\"}"}}]}],"tools":[{"schema":9007199254740993}]}`)
	out, err := PrepareBody(raw, "passthrough", "", true)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	json.Unmarshal(raw, &before)
	json.Unmarshal(out, &after)
	var bm, am []json.RawMessage
	json.Unmarshal(before["messages"], &bm)
	json.Unmarshal(after["messages"], &am)
	for _, i := range []int{2, 3} {
		if string(bm[i]) != string(am[i]) {
			t.Fatal("non-system message changed")
		}
	}
	if string(before["tools"]) != string(after["tools"]) {
		t.Fatal("tool definition changed")
	}
	if strings.Contains(string(out), "cc_entrypoint=cli") || !strings.Contains(string(out), "Keep 11128 and cc_version=42;") || !strings.Contains(string(out), "CLI tool for Claude") || !strings.Contains(string(out), "9007199254740993") {
		t.Fatalf("wrong narrow cleaning: %s", out)
	}
	twice, err := PrepareBody(out, "passthrough", "", true)
	if err != nil || string(twice) != string(out) {
		t.Fatal("cleaning must be idempotent")
	}
}

func TestSystemCleaningUnknownShapesAndQuotedHeader(t *testing.T) {
	for _, content := range []string{`{"type":"future","data":9007199254740993}`, `"  Keep x-anthropic-billing-header: value; and cc_version=42; 11128  "`, `null`} {
		raw := []byte(`{"messages":[{"role":"system","content":` + content + `}]}`)
		out, err := PrepareBody(raw, "passthrough", "", true)
		if err != nil || string(out) != string(raw) {
			t.Fatalf("unknown shape or quoted text changed: %s %v", out, err)
		}
	}
}
