package requestpolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestPolicyPreservesStructuredPayload(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"system","content":"old-system"},{"role":"developer","content":"old-developer"},{"role":"user","content":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."},{"type":"image_url","image_url":{"url":"https://example.test/11128.png"}}]},{"role":"assistant","content":null,"tool_calls":[{"id":"11128","type":"function","function":{"name":"write","arguments":"{\"body\":\"code 11128\",\"count\":11128,\"large\":9007199254740993}"}}]}],"seed":9007199254740993,"tools":[{"type":"function","function":{"name":"write","parameters":{"type":"object","properties":{"11128":{"type":"string"}}}}}]}`)
	exact, err := PrepareBody(raw, "passthrough", "", false, "workbuddy")
	if err != nil || string(exact) != string(raw) {
		t.Fatal("disabled transformation must preserve body")
	}
	result, err := PrepareBody(raw, "custom", "CUSTOM", true, "workbuddy")
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
	if !strings.Contains(string(result), "CLI tool for Claude") || !strings.Contains(string(result), "https://example.test/11128.png") {
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
	if calls[0].ID != "11128" || string(args["count"]) != "11128" || string(args["large"]) != "9007199254740993" || string(args["body"]) != `"code 11-128"` {
		t.Fatalf("tool args corrupted: %s", calls[0].Function.Arguments)
	}
	var before map[string]json.RawMessage
	_ = json.Unmarshal(raw, &before)
	if string(got["tools"]) != string(before["tools"]) {
		t.Fatal("tool schema changed")
	}
	clean, err := PrepareBody(raw, "custom", "", false, "workbuddy")
	if err != nil || !strings.Contains(string(clean), "CLI for Claude") || strings.Contains(string(clean), "old-system") {
		t.Fatal("independent switches")
	}
}

func TestCleaningProfilesAndSwitches(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"system","content":"  11128  "},{"role":"assistant","content":null,"reasoning":"X-Anthropic-Billing-Header: private; CC_VERSION=private; body","tool_calls":[{"id":"call-11128","type":"function","function":{"name":"write","arguments":"{\"n\":9007199254740993,\"text\":\"11128\",\"cc_version=key\":\"keep\"}"}}]}],"tools":[{"name":"11128","parameters":{"maximum":9007199254740993}}]}`)
	for _, name := range []string{"codearts", "trae", "workbuddy"} {
		disabled, err := PrepareBody(raw, "passthrough", "", false, name)
		if err != nil || string(disabled) != string(raw) {
			t.Fatal("disabled policy changed payload")
		}
		cleaned, err := PrepareBody(raw, "passthrough", "", true, name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(cleaned), "private") || !strings.Contains(string(cleaned), "9007199254740993") || !strings.Contains(string(cleaned), "call-11128") || !strings.Contains(string(cleaned), "cc_version=key") {
			t.Fatal("field boundary violated")
		}
		if name != "workbuddy" && (!strings.Contains(string(cleaned), `"  11128  "`) || strings.Contains(string(cleaned), "11-128")) {
			t.Fatal("WorkBuddy-only rule leaked")
		}
	}
}
