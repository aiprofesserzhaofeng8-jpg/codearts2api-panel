package requestpolicy

import (
	"fmt"
	"strings"
	"testing"
)

// Models long user context with a small, ordinary system prompt. Neither
// contains a client fingerprint, so passthrough should avoid rewriting it.
func BenchmarkPrepareBody(b *testing.B) {
	for _, size := range []int{1024, 1 << 20, 8 << 20} {
		raw := []byte(`{"messages":[{"role":"system","content":"Follow the user's instructions."},{"role":"user","content":"` + strings.Repeat("a", size) + `"}]}`)
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.SetBytes(int64(len(raw)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := PrepareBody(raw, "passthrough", "", true); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
