package requestpolicy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSummaryDetectionKeepsExplicitToolsAndRoleBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body     string
		explicit, want bool
	}{
		{"system", `{"messages":[{"role":"system","content":"You are a context summarization assistant. Test"}]}`, false, true},
		{"developer", `{"messages":[{"role":"developer","content":"You are a context summarization assistant."}]}`, false, true},
		{"user-quote", `{"messages":[{"role":"user","content":"You are a context summarization assistant."}]}`, false, false},
		{"prefix-only", `{"messages":[{"role":"system","content":"Quoted: You are a context summarization assistant."}]}`, false, false},
		{"structured-system", `{"messages":[{"role":"system","content":[{"type":"text","text":"You are a context summarization assistant."}]}]}`, false, false},
		{"explicit", `{"messages":[{"role":"user","content":"history"}]}`, true, true},
		{"tools-override-explicit", `{"tools":[{"type":"function"}],"messages":[]}`, true, false},
		{"tools-override-pi", `{"tools":[{"type":"function"}],"messages":[{"role":"system","content":"You are a context summarization assistant."}]}`, false, false},
		{"malformed", `{`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSummaryRequest([]byte(tc.body), tc.explicit); got != tc.want {
				t.Fatalf("summary classification %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSummaryPreparationKeepsBusinessDataAndCallerCeiling(t *testing.T) {
	for _, retry := range []bool{false, true} {
		body := []byte(`{"messages":[{"role":"developer","content":"Preserve headings"},{"role":"user","content":[{"type":"text","text":"cc_version=42; 11128"},{"type":"future","n":9007199254740993}]},{"role":"assistant","reasoning_content":"untouched","tool_calls":[{"id":"call-1","function":{"arguments":"{\"n\":9007199254740993}"}}]}],"seed":9007199254740993,"max_tokens":512,"max_completion_tokens":256,"reasoning_effort":"high","thinking":{"type":"enabled"},"stream_options":{"future":9007199254740993}}`)
		original := append([]byte(nil), body...)
		out, err := PrepareSummary(body, SummaryOptions{MaxTokens: 1024, DisableThinking: true}, retry)
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]json.RawMessage
		json.Unmarshal(body, &before)
		json.Unmarshal(out, &after)
		var oldMessages, newMessages []json.RawMessage
		json.Unmarshal(before["messages"], &oldMessages)
		json.Unmarshal(after["messages"], &newMessages)
		for _, i := range []int{1, 2} {
			var oldFields, newFields map[string]json.RawMessage
			json.Unmarshal(oldMessages[i], &oldFields)
			json.Unmarshal(newMessages[i], &newFields)
			if len(oldFields) != len(newFields) {
				t.Fatal("summary dropped message fields")
			}
			for name, raw := range oldFields {
				if !bytes.Equal(raw, newFields[name]) {
					t.Fatal("summary changed business/tool content")
				}
			}
		}
		if string(after["seed"]) != "9007199254740993" || string(after["max_tokens"]) != "256" || after["max_completion_tokens"] != nil || after["reasoning_effort"] != nil || string(after["thinking"]) != `{"type":"disabled"}` || string(after["stream"]) != "true" || !strings.Contains(string(after["stream_options"]), `"include_usage":true`) || !strings.Contains(string(after["stream_options"]), "9007199254740993") {
			t.Fatal("summary lost precision, caller ceiling or stream/thinking options")
		}
		guidance := SummaryGuidance
		if retry {
			guidance = SummaryRetryGuidance
		}
		if !strings.Contains(string(out), guidance) || !bytes.Equal(body, original) {
			t.Fatal("summary guidance missing or original mutated")
		}
		again, err := PrepareSummary(out, SummaryOptions{MaxTokens: 1024, DisableThinking: true}, retry)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatal("repeated preparation must not add guidance or alter output")
		}
	}
}

func TestSummaryPreparationAddsSystemAndPreservesUncontrolledThinking(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"history"}],"max_tokens":0,"max_completion_tokens":-1,"reasoning_effort":"high","thinking":{"future":9007199254740993}}`)
	out, err := PrepareSummary(body, SummaryOptions{MaxTokens: 128}, false)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	json.Unmarshal(out, &got)
	if string(got["max_tokens"]) != "128" || string(got["reasoning_effort"]) != `"high"` || string(got["thinking"]) != `{"future":9007199254740993}` || !strings.Contains(string(out), `"role":"system"`) {
		t.Fatal("added system or unconstrained fields changed")
	}
	for _, body := range [][]byte{[]byte(`{`), []byte(`{"messages":true}`)} {
		if _, err := PrepareSummary(body, SummaryOptions{MaxTokens: 128}, false); err == nil {
			t.Fatal("malformed summary accepted")
		}
	}
}
