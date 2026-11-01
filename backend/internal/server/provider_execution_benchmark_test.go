package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
	"codearts2api/internal/store"
	"codearts2api/internal/trae"
)

// The same fixture is copied into a temporary baseline checkout for comparison.
// Native Trae translation, credential lookup, policy, aggregation and accounting
// all run; a fake transport removes network and model timing from the benchmark.
func BenchmarkTraeStreamSuccess(b *testing.B) {
	for _, stream := range []bool{false, true} {
		b.Run(fmt.Sprintf("stream=%v", stream), func(b *testing.B) {
			st, err := store.Open(filepath.Join(b.TempDir(), "state.json"))
			if err != nil {
				b.Fatal(err)
			}
			st.UpdateSettings(func(c *store.Settings) { c.RateLimitEnabled = false; c.MaxRetries = 3 })
			raw, _ := json.Marshal(trae.Credentials{AccessToken: "bench", UID: "bench", Scope: "cn", ClientID: trae.ClientID, MachineID: "machine", DeviceID: "device", ExpiresAt: time.Now().Add(time.Hour).Unix()})
			st.UpsertAccount(&accounts.Account{ID: "bench", Enabled: true, Provider: provider.Trae, Scope: "cn", ExternalID: "bench", Credentials: raw})
			s := newConfiguredServer(st)
			s.Reg.ReplaceProvider(provider.Trae, "cn", []provider.Model{{ID: "shared", StreamOnly: true}})
			fixture := strings.Repeat("event: output\ndata: {\"response\":\"text\"}\n\n", 100) + "event: token_usage\ndata: {\"prompt_tokens\":12,\"completion_tokens\":4}\n\nevent: done\ndata: {\"finish_reason\":\"stop\"}\n\n"
			client := trae.New()
			client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return responseUpstream(r, fixture), nil })}
			s.Providers = provider.NewRegistry()
			if err := s.Providers.Register(client); err != nil {
				b.Fatal(err)
			}
			key, handler := st.AddKey("benchmark").Key, s.Handler()
			body := fmt.Sprintf(`{"model":"trae/cn/shared","stream":%v,"messages":[{"role":"user","content":"hi"}]}`, stream)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+key)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != 200 || !stream && !strings.Contains(w.Body.String(), "texttext") || stream && !strings.HasSuffix(w.Body.String(), "data: [DONE]\n\n") {
					b.Fatalf("completion failed: %d", w.Code)
				}
			}
		})
	}
}

// Measure both socket hops with a fixed delay; assert request count and leases
// as well as success so retries cannot make a latency result look correct.
func TestAcceptanceStreamProviderCapacity(t *testing.T) {
	for _, platform := range []string{provider.WorkBuddy, provider.Trae} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", platform, stream), func(t *testing.T) {
				var active, peak, calls atomic.Int32
				s, key, model, account := interruptionServer(t, platform, func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					calls.Add(1)
					n := active.Add(1)
					defer active.Add(-1)
					for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
					}
					select {
					case <-time.After(20 * time.Millisecond):
					case <-r.Context().Done():
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, interruptionPayload(platform, "fixture-complete", true))
				})
				s.Store.UpdateSettings(func(c *store.Settings) { c.RateLimitEnabled = false; c.Pool.MaxInFlight = 2; c.MaxRetries = 1 })
				s.ApplyRateLimit()
				gateway := httptest.NewServer(s.Handler())
				defer gateway.Close()
				client := gateway.Client()
				client.Timeout = 3 * time.Second
				body := fmt.Sprintf(`{"model":%q,"stream":%v,"messages":[{"role":"user","content":"hi"}]}`, model, stream)
				latencies := make(chan float64, 40)
				var failures atomic.Int32
				for range 20 {
					var wg sync.WaitGroup
					for range 2 {
						wg.Go(func() {
							started := time.Now()
							req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
							req.Header.Set("Authorization", "Bearer "+key)
							resp, err := client.Do(req)
							if err != nil {
								failures.Add(1)
								return
							}
							raw, err := io.ReadAll(resp.Body)
							resp.Body.Close()
							if err != nil || resp.StatusCode != 200 || !strings.Contains(string(raw), "fixture-complete") || !stream && !acceptanceCompletionOK(raw, "fixture-complete") {
								failures.Add(1)
							}
							latencies <- float64(time.Since(started).Microseconds()) / 1000
						})
					}
					wg.Wait()
				}
				close(latencies)
				var times []float64
				for elapsed := range latencies {
					times = append(times, elapsed)
				}
				sort.Float64s(times)
				if len(times) != 40 || calls.Load() != 40 || peak.Load() != 2 || failures.Load() != 0 || s.Store.PoolSnapshot()[account].InFlight != 0 {
					t.Fatalf("capacity/count/lease failure: samples=%d calls=%d peak=%d failures=%d", len(times), calls.Load(), peak.Load(), failures.Load())
				}
				data, _ := json.Marshal(map[string]any{"provider": platform, "stream": stream, "samples": 40, "p95_ms": times[37], "upstream_delay_ms": 20, "concurrency": 2, "peak": peak.Load(), "calls": calls.Load(), "failures": failures.Load(), "remaining_leases": 0})
				fmt.Printf("PROVIDER_PERF %s\n", data)
			})
		}
	}
}
