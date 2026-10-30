package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// The exact same benchmark also runs against the frozen baseline. It includes
// authentication, retries/leases, SSE decoding, forwarding and the final log.
// The upstream is controlled: these numbers do not measure provider latency.
func BenchmarkGatewayStreamTiming(b *testing.B) {
	st, err := store.Open(filepath.Join(b.TempDir(), "state.json"))
	if err != nil {
		b.Fatal(err)
	}
	st.UpdateSettings(func(s *store.Settings) { s.RateLimitEnabled = false; s.MaxRetries = 1 })
	st.UpsertAccount(&codearts.Account{ID: "bench", Enabled: true, Status: "ok", Session: &codearts.Session{AccessKey: "ak", SecretKey: "sk", ExpiresAt: time.Now().Add(time.Hour)}})
	s := New(st)
	s.Reg.Replace([]registry.Model{{ID: "test-model", StreamOnly: true}})
	key := st.AddKey("benchmark").Key
	fixture := strings.Repeat("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"text\"}}]}\n\n", 100) + "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n"
	s.Up.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return responseUpstream(r, fixture), nil })}
	h := s.Handler()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.HasSuffix(w.Body.String(), "data: [DONE]\n\n") {
			b.Fatalf("stream failed: %d", w.Code)
		}
	}
}
