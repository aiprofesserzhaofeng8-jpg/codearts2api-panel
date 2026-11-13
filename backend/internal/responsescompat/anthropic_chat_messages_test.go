// Adapted contract fixtures for the extracted Sub2API bridge. LGPL-3.0;
// see LICENSE, COPYING, UPSTREAM.md and messages-upstream.json.
package apicompat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessagesBridgeToolHistoryAndPrecision(t *testing.T) {
	msgs, err := AnthropicMessageToChatMessages(AnthropicMessage{Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"before"},{"type":"thinking","thinking":"after"},{"type":"tool_use","id":"call-1","name":"read","input":{"id":9007199254740993}}]`)})
	if err != nil || len(msgs) != 1 || len(msgs[0].ToolCalls) != 1 {
		t.Fatal(msgs, err)
	}
	call := msgs[0].ToolCalls[0]
	if call.ID != "call-1" || call.Function.Name != "read" || call.Function.Arguments != `{"id":9007199254740993}` || msgs[0].ReasoningContent != "before\nafter" {
		t.Fatal(msgs)
	}
	// This is upstream's narrower policy. protocol's adapter deliberately retains
	// plain reasoning history too, covered by the R3 boundary regression.
	msgs, err = AnthropicMessageToChatMessages(AnthropicMessage{Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"reason"},{"type":"text","text":"visible"}]`)})
	if err != nil || msgs[0].ReasoningContent != "" || string(msgs[0].Content) != `"visible"` {
		t.Fatal(msgs, err)
	}
}

func TestMessagesBridgeToolResultMediaAttribution(t *testing.T) {
	msgs, err := AnthropicMessageToChatMessages(AnthropicMessage{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call-1","content":[{"type":"text","text":"result"},{"type":"image","source":{"type":"url","url":"https://example.test/a.png"}}]}]`)})
	if err != nil || len(msgs) != 2 || msgs[0].Role != "tool" || msgs[0].ToolCallID != "call-1" || string(msgs[0].Content) != `"result"` {
		t.Fatal(msgs, err)
	}
	var parts []ChatContentPart
	if json.Unmarshal(msgs[1].Content, &parts) != nil || len(parts) != 2 || !strings.Contains(parts[0].Text, "call-1") || parts[1].ImageURL.URL != "https://example.test/a.png" {
		t.Fatal(msgs)
	}
	empty, err := AnthropicMessageToChatMessages(AnthropicMessage{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call-2","content":""}]`)})
	if err != nil || string(empty[0].Content) != `""` {
		t.Fatal("invented empty tool result", empty, err)
	}
}
