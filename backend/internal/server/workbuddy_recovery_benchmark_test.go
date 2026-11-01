package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
	"codearts2api/internal/workbuddy"
)

// Run this identical benchmark on the frozen baseline and the changed tree.
// It covers the normal successful path with recovery enabled, without network
// or real-provider latency hiding gateway overhead.
func BenchmarkWorkBuddyRecoverySuccess(b *testing.B) {
	for _, stream := range []bool{false, true} {
		b.Run(fmt.Sprintf("stream=%v", stream), func(b *testing.B) {
			st, err := store.Open(filepath.Join(b.TempDir(), "state.json"))
			if err != nil {
				b.Fatal(err)
			}
			st.UpdateSettings(func(s *store.Settings) { s.RateLimitEnabled = false; s.MaxRetries = 3 })
			raw, _ := json.Marshal(workbuddy.Credentials{AccessToken: "bench-access", RefreshToken: "bench-refresh", UID: "bench", Realm: "cn", ExpiresAt: time.Now().Add(time.Hour).Unix()})
			st.UpsertAccount(&codearts.Account{ID: "bench", Enabled: true, Provider: provider.WorkBuddy, Scope: "cn", ExternalID: "bench", Credentials: raw})
			s := New(st)
			s.Reg.ReplaceProvider(provider.WorkBuddy, "cn", []provider.Model{{ID: "shared", StreamOnly: true}})
			key := st.AddKey("benchmark").Key
			fixture := strings.Repeat("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"text\"}}]}\n\n", 100) + "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n"
			client := workbuddy.New()
			client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return responseUpstream(r, fixture), nil })}
			s.Providers = provider.NewRegistry()
			if err := s.Providers.Register(client); err != nil {
				b.Fatal(err)
			}
			h := s.Handler()
			body := fmt.Sprintf(`{"model":"workbuddy/cn/shared","stream":%v,"messages":[{"role":"user","content":"hi"}]}`, stream)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+key)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "texttext") && !stream || stream && !strings.HasSuffix(w.Body.String(), "data: [DONE]\n\n") {
					b.Fatalf("completion failed: %d", w.Code)
				}
			}
		})
	}
}
