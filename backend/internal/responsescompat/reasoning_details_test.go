package apicompat

import (
	"encoding/json"
	"strings"
	"testing"
)

const opaqueDetails = `[{"type":"reasoning.encrypted","id":"a","index":0,"data":"opaque","number":9007199254740993}]`

func TestOpaqueDetailsResponsesRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var output []ResponsesOutput
		if stream {
			state := NewChatCompletionsToResponsesStreamState("test")
			var chunk ChatCompletionsChunk
			json.Unmarshal([]byte(`{"choices":[{"index":0,"delta":{"reasoning_details":`+opaqueDetails+`,"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`), &chunk)
			events := ChatCompletionsChunkToResponsesEvents(&chunk, state)
			events = append(events, FinalizeChatCompletionsResponsesStream(state)...)
			done := false
			for _, e := range events {
				if e.Type == "response.output_item.done" && e.Item != nil && e.Item.Type == "reasoning" {
					b, _ := json.Marshal(e.Item)
					if !strings.Contains(string(b), "9007199254740993") {
						t.Fatal(string(b))
					}
					done = true
				}
			}
			if !done {
				t.Fatal("reasoning item never completed")
			}
			output = state.chatOutput()
		} else {
			var response ChatCompletionsResponse
			json.Unmarshal([]byte(`{"choices":[{"message":{"role":"assistant","reasoning_details":`+opaqueDetails+`,"tool_calls":[{"id":"call1","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`), &response)
			output = ChatCompletionsResponseToResponses(&response, "test", nil, nil, false, nil).Output
		}
		b, _ := json.Marshal(output)
		if strings.Contains(string(b), "encrypted_content") || !strings.Contains(string(b), "reasoning_details") {
			t.Fatal("invented signature or dropped extension", string(b))
		}
		var input []json.RawMessage
		json.Unmarshal(b, &input)
		input = append(input, json.RawMessage(`{"type":"function_call_output","call_id":"call1","output":"result"}`))
		raw, _ := json.Marshal(input)
		req, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{Model: "test", Input: raw})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range req.Messages {
			if len(m.ToolCalls) > 0 {
				d, _ := json.Marshal(m.ReasoningDetails)
				if string(d) != opaqueDetails {
					t.Fatal(string(d))
				}
				found = true
			}
		}
		if !found {
			t.Fatal("tool missing")
		}
	}
}
func TestOpaqueDetailsDoNotCrossUserTurn(t *testing.T) {
	input := json.RawMessage(`[{"type":"reasoning","reasoning_details":` + opaqueDetails + `},{"type":"message","role":"user","content":"new turn"},{"type":"function_call","call_id":"c","name":"read","arguments":"{}"}]`)
	req, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{Model: "test", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range req.Messages {
		if len(m.ReasoningDetails) > 0 {
			t.Fatal("stale metadata crossed turn")
		}
	}
}
