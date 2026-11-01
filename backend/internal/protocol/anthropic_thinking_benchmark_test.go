package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// This benchmark only uses the pre-existing Request/ToOpenAI API, so the same
// file can run unchanged against the frozen version for a fair cost comparison.
func BenchmarkMessagesThinkingConversion(b *testing.B) {
	for _, size := range []struct{ name, text string }{{"short", "hello"}, {"16KiB", strings.Repeat("x", 16<<10)}} {
		for _, pref := range []string{"omitted", "disabled"} {
			b.Run(size.name+"/"+pref, func(b *testing.B) {
				obj := map[string]any{"model": "deepseek-v4.1-flash", "max_tokens": 64, "messages": []map[string]string{{"role": "user", "content": size.text}}}
				if pref == "disabled" {
					obj["thinking"] = map[string]string{"type": "disabled"}
				}
				raw, _ := json.Marshal(obj)
				b.ReportAllocs()
				b.SetBytes(int64(len(raw)))
				for b.Loop() {
					var req Request
					if err := json.Unmarshal(raw, &req); err != nil {
						b.Fatal(err)
					}
					if _, err := ToOpenAI(&req, "deepseek-v4.1-flash"); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
