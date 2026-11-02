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

	"codearts2api/internal/codearts"
	"codearts2api/internal/registry"
	"codearts2api/internal/requestpolicy"
	"codearts2api/internal/storage"
)

// The identical fixture is compiled against the frozen baseline. The signed
// native client, summary retry, private buffering, usage and final HTTP reply run.
func BenchmarkSummaryExecution(b *testing.B) {
	for _, size := range []int{1024, 1 << 20} {
		for _, retry := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				b.Run(fmt.Sprintf("size=%d/retry=%v/stream=%v", size, retry, stream), func(b *testing.B) {
					st, err := storage.Open(filepath.Join(b.TempDir(), "state.json"))
					if err != nil {
						b.Fatal(err)
					}
					st.UpdateSettings(func(settings *storage.Settings) { settings.RateLimitEnabled = false })
					s := newConfiguredServer(st)
					s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}})
					sess := &codearts.Session{AccessKey: "benchmark-ak", SecretKey: "benchmark-sk", ExpiresAt: time.Now().Add(time.Hour)}
					st.UpsertAccount(&codearts.Account{ID: "summary-benchmark", Enabled: true, NativeCredential: sess})
					s.Up.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						raw, _ := io.ReadAll(r.Body)
						if retry && !strings.Contains(string(raw), requestpolicy.SummaryRetryGuidance) {
							return responseUpstream(r, summaryFixture("PRIVATE_PARTIAL", "length", 3, 1)), nil
						}
						return responseUpstream(r, summaryFixture("COMPLETE_SUMMARY", "stop", 4, 2)), nil
					})}
					key := st.AddKey("summary-benchmark").Key
					handler := s.Handler()
					body := fmt.Sprintf(`{"model":"deepseek-v4.1-flash","stream":%v,"max_tokens":512,"messages":[{"role":"system","content":"You are a context summarization assistant."},{"role":"user","content":%q}]}`, stream, strings.Repeat("h", size))
					b.ReportAllocs()
					b.SetBytes(int64(len(body)))
					b.ResetTimer()
					for range b.N {
						req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
						req.Header.Set("Authorization", "Bearer "+key)
						response := httptest.NewRecorder()
						handler.ServeHTTP(response, req)
						if response.Code != 200 || !strings.Contains(response.Body.String(), "COMPLETE_SUMMARY") || strings.Contains(response.Body.String(), "PRIVATE_PARTIAL") {
							b.Fatalf("summary failed: %d", response.Code)
						}
					}
				})
			}
		}
	}
}

// Both HTTP hops are sockets with fixed upstream time. Counts distinguish the
// intentional truncation retry from accidental replay or extra account failover.
func TestAcceptanceSummaryCapacity(t *testing.T) {
	for _, retry := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("retry=%v/stream=%v", retry, stream), func(t *testing.T) {
				var active, peak, calls atomic.Int32
				s, key, _, account := interruptionServer(t, "codearts", func(w http.ResponseWriter, r *http.Request) {
					raw, _ := io.ReadAll(r.Body)
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
					if retry && !strings.Contains(string(raw), requestpolicy.SummaryRetryGuidance) {
						io.WriteString(w, summaryFixture("PRIVATE_PARTIAL", "length", 3, 1))
						return
					}
					io.WriteString(w, summaryFixture("COMPLETE_SUMMARY", "stop", 4, 2))
				})
				s.Reg.Replace([]registry.Model{{ID: "deepseek-v4.1-flash"}})
				s.Store.UpdateSettings(func(c *storage.Settings) { c.RateLimitEnabled = false; c.Pool.MaxInFlight = 2; c.MaxRetries = 1 })
				s.ApplyRateLimit()
				gateway := httptest.NewServer(s.Handler())
				defer gateway.Close()
				client := gateway.Client()
				client.Timeout = 3 * time.Second
				body := fmt.Sprintf(`{"model":"deepseek-v4.1-flash","stream":%v,"max_tokens":512,"messages":[{"role":"system","content":"You are a context summarization assistant."},{"role":"user","content":"history"}]}`, stream)
				times := make(chan float64, 40)
				var failures atomic.Int32
				firstOutputs := make(chan float64, 40)
				batchStarted := time.Now()
				for range 20 {
					var wg sync.WaitGroup
					for range 2 {
						wg.Go(func() {
							started := time.Now()
							req, _ := http.NewRequestWithContext(context.Background(), "POST", gateway.URL+"/v1/chat/completions", strings.NewReader(body))
							req.Header.Set("Authorization", "Bearer "+key)
							response, err := client.Do(req)
							if err != nil {
								failures.Add(1)
								return
							}
							raw, first, err := acceptanceReadOutput(response.Body, started, "COMPLETE_SUMMARY")
							firstOutputs <- first
							response.Body.Close()
							if err != nil || first <= 0 || response.StatusCode != 200 || !strings.Contains(string(raw), "COMPLETE_SUMMARY") || strings.Contains(string(raw), "PRIVATE_PARTIAL") {
								failures.Add(1)
							}
							times <- float64(time.Since(started).Microseconds()) / 1000
						})
					}
					wg.Wait()
				}
				elapsed := time.Since(batchStarted)
				close(firstOutputs)
				firstTimes := acceptanceTimes(firstOutputs)
				close(times)
				var latencies []float64
				for value := range times {
					latencies = append(latencies, value)
				}
				sort.Float64s(latencies)
				expected := 40
				if retry {
					expected = 80
				}
				if len(latencies) != 40 || calls.Load() != int32(expected) || peak.Load() != 2 || failures.Load() != 0 || s.Store.PoolSnapshot()[account].InFlight != 0 {
					t.Fatalf("summary count/capacity/lease failure: samples=%d calls=%d peak=%d failures=%d", len(latencies), calls.Load(), peak.Load(), failures.Load())
				}
				data, _ := json.Marshal(map[string]any{"provider": "codearts_summary", "retry": retry, "stream": stream, "samples": 40, "p95_ms": latencies[37], "p50_ms": latencies[19], "requests_per_second": 40 / elapsed.Seconds(), "first_output_p95_ms": firstTimes[37], "upstream_delay_ms": 20, "concurrency": 2, "peak": peak.Load(), "calls": calls.Load(), "expected_calls": expected, "failures": failures.Load(), "remaining_leases": 0})
				fmt.Printf("SUMMARY_PERF %s\n", data)
			})
		}
	}
}
