package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAggregateKeepsToolExtraContent(t *testing.T) {
	out, err := AggregateSSE([][]byte{
		[]byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"read","arguments":"{\"n\":"},"extra_content":{"google":{"thought_signature":"OPAQUE"}}}]}}]}`),
		[]byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"9007199254740993}"}}]},"finish_reason":"tool_calls"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Choices []struct {
			Message struct {
				Calls []struct {
					Extra    json.RawMessage `json:"extra_content"`
					Function struct {
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(out, &reply) != nil || len(reply.Choices) != 1 || len(reply.Choices[0].Message.Calls) != 1 {
		t.Fatal(string(out))
	}
	c := reply.Choices[0].Message.Calls[0]
	if !strings.Contains(string(c.Extra), "OPAQUE") || c.Function.Arguments != `{"n":9007199254740993}` {
		t.Fatal(string(out))
	}
}
