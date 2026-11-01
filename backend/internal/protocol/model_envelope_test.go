package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMappedModelEnvelopePreservesExactPayloadValues(t *testing.T) {
	original := []byte(`{"model":"upstream","seed":9007199254740993,"choices":[{"message":{"tool_calls":[{"id":"call","function":{"arguments":"{\"n\":9007199254740993}"}}],"refusal":"合法拒绝","reasoning_content":"思考"}}],"unknown":{"integer":18446744073709551615}}`)
	for _, rewrite := range []func([]byte, string) ([]byte, error){MappedRequestModel, MappedResponseModel} {
		out, err := rewrite(original, "deepseek-4.1-flash")
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]json.RawMessage
		_ = json.Unmarshal(original, &before)
		_ = json.Unmarshal(out, &after)
		if string(after["model"]) != `"deepseek-4.1-flash"` || len(after) != len(before) {
			t.Fatal(string(out))
		}
		for key, value := range before {
			if key != "model" && string(value) != string(after[key]) {
				t.Fatalf("%s was changed: %s", key, out)
			}
		}
	}
	for _, raw := range []string{"null", "[]", "{broken"} {
		if _, err := MappedResponseModel([]byte(raw), "name"); err == nil {
			t.Fatal("invalid response accepted", raw)
		}
	}
}

func TestSearchQuerySelectsLastNonemptyUserText(t *testing.T) {
	var req SearchMessagesRequest
	raw := `{"messages":[{"role":"user","content":"older"},{"role":"assistant","content":"ignore"},{"role":"user","content":[{"type":"image","text":"ignore"},{"type":"text","text":"perform a WEB search for the query:"},{"type":"text","text":"具体问题"}]},{"role":"user","content":"  "},{"role":"user","content":{"unknown":true}}]}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if got := SearchQuery(req); got != "具体问题" {
		t.Fatal(got)
	}
	req.Messages[0].Content = json.RawMessage(`"plain"`)
	req.Messages = req.Messages[:1]
	if got := SearchQuery(req); strings.TrimSpace(got) != "plain" {
		t.Fatal(got)
	}
}
