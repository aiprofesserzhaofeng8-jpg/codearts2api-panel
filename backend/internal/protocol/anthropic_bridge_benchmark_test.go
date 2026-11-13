package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func BenchmarkMessagesBridgeHistory(b *testing.B) {
	for _, size := range []int{1024, 65536} {
		name := "1KiB"
		if size > 1024 {
			name = "64KiB"
		}
		b.Run(name, func(b *testing.B) {
			user, _ := json.Marshal(strings.Repeat("x", size))
			req := Request{Messages: []Message{{Role: "user", Content: user}, {Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"reason"},{"type":"tool_use","id":"call-1","name":"read","input":{"id":9007199254740993}}]`)}, {Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call-1","content":"RESULT"}]`)}}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := ToOpenAI(&req, "model"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
