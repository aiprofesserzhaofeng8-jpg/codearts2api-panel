package server

import (
	"bytes"
	"codearts2api/internal/store"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Uses signed gateway requests and a controlled upstream. These measurements are
// gateway overhead under this fixture, not Huawei latency/capacity claims.
func TestAcceptanceGatewayLatencyAndCapacity(t *testing.T) {
	s := poolServer(t, 1)
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
		return responseUpstream(r, summaryFixture("fixture-complete", "stop", 12, 4)), nil
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
				if resp.StatusCode != 200 || !bytes.Contains(raw, []byte("fixture-complete")) {
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
