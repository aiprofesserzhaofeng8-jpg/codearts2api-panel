package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAggregateOpaqueReasoningDetailsPreservesOrderAndNumbers(t *testing.T) {
	chunks := [][]byte{
		[]byte(`{"choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.encrypted","id":"a","index":0,"data":"first","vendor_number":9007199254740993}]}}]}`),
		[]byte(`{"choices":[{"index":1,"delta":{"reasoning_details":[{"type":"reasoning.encrypted","data":"other-choice"}]}},{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.encrypted","id":"b","data":"second"}],"content":"OK"},"finish_reason":"stop"}]}`),
	}
	b, err := AggregateSSE(chunks)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string            `json:"content"`
				Details []json.RawMessage `json:"reasoning_details"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Choices) != 2 || len(result.Choices[0].Message.Details) != 2 || len(result.Choices[1].Message.Details) != 1 || result.Choices[0].Message.Content != "OK" || !strings.Contains(string(result.Choices[0].Message.Details[0]), "9007199254740993") || strings.Contains(result.Choices[0].Message.Content, "first") {
		t.Fatal(string(b))
	}
}

func TestOpaqueDetailsMessagesRoundTrip(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"role":"assistant","reasoning_details":[{"type":"reasoning.encrypted","data":"opaque","number":9007199254740993}],"tool_calls":[{"id":"c","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	reply := FromOpenAI(raw, "test")
	b, _ := json.Marshal(reply.Payload["content"])
	if strings.Contains(string(b), "signature") || !strings.Contains(string(b), "9007199254740993") {
		t.Fatal(string(b))
	}
	req := Request{Model: "test", MaxTokens: 64, Messages: []Message{{Role: "assistant", Content: b}, {Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"c","content":"result"}]`)}}}
	out, err := ToOpenAI(&req, "test")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(out)
	if !strings.Contains(string(body), `"reasoning_details":[{"data":"opaque","number":9007199254740993,"type":"reasoning.encrypted"}]`) {
		t.Fatal(string(body))
	}
}
