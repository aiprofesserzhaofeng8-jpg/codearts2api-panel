package server

import (
	"bytes"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Uses signed gateway requests and a controlled upstream. These measurements are
// gateway overhead under this fixture, not Huawei latency/capacity claims.
func TestAcceptanceGatewayLatencyAndCapacity(t *testing.T) {
	s := poolServer(t, 1)
	s.Reg.Replace([]registry.Model{{ID: "test-model"}})
	s.Store.UpdateSettings(func(c *store.Settings) { c.RateLimitEnabled = false; c.MaxRetries = 1 })
	s.ApplyRateLimit()
	key := s.Store.AddKey("acceptance fixture").Key
	var active, peak atomic.Int32
	s.Up.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return nonJSONResponse(r, `{"choices":[{"message":{"role":"assistant","content":"fixture-complete"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`), nil
	})}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	client := server.Client()
	body := []byte(`{"model":"test-model","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var times []float64
	var mu sync.Mutex
	var failures atomic.Int32
	for round := 0; round < 50; round++ {
		var wg sync.WaitGroup
		for n := 0; n < 2; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := time.Now()
				req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+key)
				resp, err := client.Do(req)
				if err != nil {
					failures.Add(1)
					return
				}
				raw, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 || !acceptanceCompletionOK(raw, "fixture-complete") {
					failures.Add(1)
				}
				mu.Lock()
				times = append(times, float64(time.Since(start).Microseconds())/1000)
				mu.Unlock()
			}()
		}
		wg.Wait()
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	sort.Float64s(times)
	if len(times) != 100 {
		t.Fatalf("completed samples=%d", len(times))
	}
	p95 := times[94]
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	metrics := map[string]any{"samples": len(times), "concurrency": 2, "upstream_delay_ms": 20, "p95_ms": p95, "peak_upstream_concurrency": peak.Load(), "heap_growth_bytes": growth, "failures": failures.Load(), "remaining_leases": s.Store.PoolSnapshot()["a"].InFlight}
	raw, _ := json.Marshal(metrics)
	fmt.Printf("QUALITY_PERF %s\n", raw)
	if failures.Load() != 0 || p95 >= 500 || peak.Load() > 2 || growth >= 32<<20 || s.Store.PoolSnapshot()["a"].InFlight != 0 {
		t.Fatal("gateway performance/capacity acceptance failed")
	}
}

// Check protocol structure rather than accepting an SSE body that merely
// contains the expected word in a non-streaming response.
func acceptanceCompletionOK(raw []byte, text string) bool {
	var reply struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
			Finish  string                   `json:"finish_reason"`
		} `json:"choices"`
	}
	return json.Unmarshal(raw, &reply) == nil && len(reply.Choices) == 1 &&
		reply.Choices[0].Message.Content == text && reply.Choices[0].Finish == "stop"
}

// A real socket at both boundaries checks increasing concurrent load without
// contacting a model. Each stream has several flushed events, not a buffered
// imitation. Report timings for comparison; only generous bounds catch hangs.
func TestAcceptanceConcurrencyLadder(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, limit := range []int{1, 4, 16} {
			t.Run(fmt.Sprintf("stream=%t/concurrency=%d", stream, limit), func(t *testing.T) {
				s := poolServer(t, 1)
				s.Reg.Replace([]registry.Model{{ID: "test-model"}})
				s.Store.UpdateSettings(func(c *store.Settings) {
					c.RateLimitEnabled = false
					c.MaxRetries = 1
					c.Pool.MaxInFlight = limit
				})
				s.ApplyRateLimit()
				key := s.Store.AddKey("controlled capacity").Key
				var active, peak, calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					n := active.Add(1)
					defer active.Add(-1)
					for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						for _, part := range []string{"fixture", "-", "complete"} {
							fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", part)
							w.(http.Flusher).Flush()
							time.Sleep(time.Millisecond)
						}
						io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n")
					} else {
						time.Sleep(3 * time.Millisecond)
						w.Header().Set("Content-Type", "application/json")
						io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"fixture-complete"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`)
					}
				}))
				defer upstream.Close()
				transport := http.DefaultTransport.(*http.Transport).Clone()
				transport.Proxy = nil
				defer transport.CloseIdleConnections()
				s.Up.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					cloned := r.Clone(r.Context())
					cloned.URL.Scheme = "http"
					cloned.URL.Host = strings.TrimPrefix(upstream.URL, "http://")
					return transport.RoundTrip(cloned)
				})}
				gateway := httptest.NewServer(s.Handler())
				defer gateway.Close()
				client := gateway.Client()
				client.Timeout = 5 * time.Second
				var latencies []float64
				for range 3 {
					var wg sync.WaitGroup
					results := make(chan float64, limit)
					errs := make(chan error, limit)
					for range limit {
						wg.Add(1)
						go func() {
							defer wg.Done()
							start := time.Now()
							req, _ := http.NewRequest("POST", gateway.URL+"/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"test-model","messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)))
							req.Header.Set("Authorization", "Bearer "+key)
							resp, err := client.Do(req)
							if err != nil {
								errs <- err
								return
							}
							raw, err := io.ReadAll(resp.Body)
							resp.Body.Close()
							valid := acceptanceCompletionOK(raw, "fixture-complete")
							if stream {
								var out strings.Builder
								parts, ends, done := 0, 0, 0
								valid = true
								for line := range strings.SplitSeq(string(raw), "\n") {
									if !strings.HasPrefix(line, "data: ") {
										continue
									}
									payload := strings.TrimPrefix(line, "data: ")
									if payload == "[DONE]" {
										done++
										continue
									}
									var chunk struct {
										Error   json.RawMessage `json:"error"`
										Choices []struct {
											Delta  struct{ Content string } `json:"delta"`
											Finish string                   `json:"finish_reason"`
										} `json:"choices"`
									}
									if json.Unmarshal([]byte(payload), &chunk) != nil || len(chunk.Error) > 0 {
										valid = false
										break
									}
									for _, ch := range chunk.Choices {
										if ch.Delta.Content != "" {
											out.WriteString(ch.Delta.Content)
											parts++
										}
										if ch.Finish == "stop" {
											ends++
										}
									}
								}
								valid = valid && out.String() == "fixture-complete" && parts == 3 && ends == 1 && done == 1
							}
							if err != nil || resp.StatusCode != 200 || !valid {
								errs <- fmt.Errorf("status=%d body=%s read=%v", resp.StatusCode, raw, err)
								return
							}
							results <- float64(time.Since(start).Microseconds()) / 1000
						}()
					}
					wg.Wait()
					close(results)
					close(errs)
					for err := range errs {
						t.Error(err)
					}
					for ms := range results {
						latencies = append(latencies, ms)
					}
				}
				sort.Float64s(latencies)
				if len(latencies) != limit*3 || calls.Load() != int32(limit*3) || peak.Load() > int32(limit) || s.Store.PoolSnapshot()["a"].InFlight != 0 {
					t.Fatalf("samples=%d calls=%d peak=%d lease=%+v", len(latencies), calls.Load(), peak.Load(), s.Store.PoolSnapshot()["a"])
				}
				if s.Store.Stats().FailedRequests != 0 || s.Store.Stats().UsageRequests != int64(limit*3) {
					t.Fatal("success/usage accounting mismatch")
				}
				t.Logf("CAPACITY stream=%t concurrency=%d samples=%d p50_ms=%.3f p95_ms=%.3f peak=%d", stream, limit, len(latencies), latencies[len(latencies)/2], latencies[(len(latencies)*95+99)/100-1], peak.Load())
			})
		}
	}
}
func TestAcceptanceCapacityRejectsAndRecovers(t *testing.T) {
	s := poolServer(t, 1)
	s.Store.UpdateSettings(func(c *store.Settings) { c.MaxRetries = 1 })
	a, r1 := s.Store.AcquireAccountForModel("", "test-model", nil)
	b, r2 := s.Store.AcquireAccountForModel("", "test-model", nil)
	if a == nil || b == nil {
		t.Fatal("fixture capacity not available")
	}
	w := responseRequest(s, s.Store.AddKey("fixture").Key, "/v1/chat/completions", `{"model":"test-model","messages":[{"role":"user","content":"hello"}]}`)
	if w.Code != 503 {
		t.Fatalf("overflow=%d", w.Code)
	}
	r1()
	r2()
	c, release := s.Store.AcquireAccountForModel("", "test-model", nil)
	if c == nil {
		t.Fatal("capacity never recovered")
	}
	release()
}
