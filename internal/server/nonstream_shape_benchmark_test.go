package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"codearts2api/internal/codearts"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// Includes signed Client.Chat, response-body read, envelope validation and
// server usage capture. Run with old/new client.go and chat.go overlays to
// compare the whole affected path; it does not measure network/model latency.
func BenchmarkNonStreamingCodeArtsOnce(b *testing.B) {
	for _, size := range []int{1024, 1 << 20, 8 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			st, err := store.Open(filepath.Join(b.TempDir(), "state.json"))
			if err != nil {
				b.Fatal(err)
			}
			body := `{"choices":[{"message":{"content":"` + strings.Repeat("x", size) + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`
			s := &Server{Store: st, Up: codearts.NewClient()}
			s.Up.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return nonJSONResponse(r, body), nil
			})}
			sess := &codearts.Session{AccessKey: "benchmark-ak", SecretKey: "benchmark-sk"}
			ctx := withUsageCapture(b.Context(), "chat")
			request := []byte(`{"messages":[{"role":"user","content":"hi"}],"stream":false}`)
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.codeartsChatOnce(ctx, sess, registry.Model{ID: "test-model"}, request, func() {}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
