package codearts

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkAggregateSSE(b *testing.B) {
	for _, size := range []int{100, 2000} {
		chunks := make([][]byte, size+1)
		for i := 0; i < size; i++ {
			chunks[i] = []byte(`{"id":"bench","model":"fixture","choices":[{"index":0,"delta":{"content":"` + strings.Repeat("x", 32) + `"}}]}`)
		}
		chunks[size] = []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":200}}`)
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := AggregateSSE(chunks); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
