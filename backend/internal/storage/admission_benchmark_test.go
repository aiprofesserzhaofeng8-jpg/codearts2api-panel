package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"codearts2api/internal/accounts"
)

// Disable affinity so every iteration exercises selection, including full scans
// in least_inflight. The mapped/sticky cases live in BenchmarkModelSelection.
func BenchmarkAdmissionSelection(b *testing.B) {
	for _, strategy := range []string{"round_robin", "least_inflight", "sequential_exhaust"} {
		for _, size := range []int{1, 13, 128} {
			b.Run(fmt.Sprintf("%s/accounts=%d", strategy, size), func(b *testing.B) {
				s, err := Open(filepath.Join(b.TempDir(), "state.json"))
				if err != nil {
					b.Fatal(err)
				}
				s.UpdateSettings(func(c *Settings) { c.Pool.Strategy = strategy; c.Pool.StickyTTLSec = 0 })
				for i := range size {
					s.UpsertAccount(&accounts.Account{ID: fmt.Sprint(i), Provider: "workbuddy", Scope: "cn", Enabled: true})
				}
				s.pool.now = func() time.Time { return time.Unix(100, 0) }
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					a, release, err := s.AcquireAccountForKey("", "workbuddy/cn/deepseek-v4.1-flash", nil, "")
					release()
					if a == nil || err != nil {
						b.Fatal("admission failed", err)
					}
				}
			})
		}
	}
}
