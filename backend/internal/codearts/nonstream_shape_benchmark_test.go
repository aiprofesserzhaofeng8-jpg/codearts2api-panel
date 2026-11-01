package codearts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Compare the combined pre-fix Client.Chat + server business-error scans with
// the replacement validation. This excludes HTTP/usage cost; the full client
// and server-path benchmarks measure those separately.
func BenchmarkChatReplyValidation(b *testing.B) {
	for _, size := range []int{1024, 1 << 20, 8 << 20} {
		body := []byte(`{"choices":[{"message":{"content":"` + strings.Repeat("x", size) + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.Run("previous_combined", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(body)))
				for i := 0; i < b.N; i++ {
					if !json.Valid(body) {
						b.Fatal("invalid fixture")
					}
					if _, _, bad := IsUpstreamBusinessError(body); bad {
						b.Fatal("business-error fixture")
					}
				}
			})
			b.Run("current", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(body)))
				for i := 0; i < b.N; i++ {
					if err := validateChatReply(body); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
