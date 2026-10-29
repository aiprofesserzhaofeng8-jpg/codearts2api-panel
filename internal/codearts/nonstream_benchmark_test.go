package codearts

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Measure the full non-streaming client path with an in-memory HTTP response,
// so baseline/fixed runs expose validation cost without network variance.
func BenchmarkNonStreamingChat(b *testing.B) {
	for _, size := range []int{1024, 1 << 20, 8 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			body := `{"choices":[{"message":{"content":"` + strings.Repeat("x", size) + `"},"finish_reason":"stop"}]}`
			client := NewClient()
			client.HTTP = &http.Client{Transport: creditRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			sess := &Session{AccessKey: "benchmark-ak", SecretKey: "benchmark-sk"}
			request := []byte(`{"messages":[],"stream":false}`)
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := client.Chat(b.Context(), sess, "test-model", request); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
