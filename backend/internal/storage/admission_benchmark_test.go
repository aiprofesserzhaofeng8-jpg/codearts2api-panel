package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"codearts2api/internal/access"
	"codearts2api/internal/accounts"
	"codearts2api/internal/config"
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

// Compare the former mapping admission path with revision-checked admission,
// with the same two sources, accounts, group and full-scan scheduler.
func BenchmarkUnifiedAdmission(b *testing.B) {
	for _, size := range []int{13, 128} {
		for _, mode := range []string{"mapped_compat", "unified"} {
			b.Run(fmt.Sprintf("%s/accounts=%d", mode, size), func(b *testing.B) {
				s, err := Open(filepath.Join(b.TempDir(), "state.json"))
				if err != nil {
					b.Fatal(err)
				}
				s.UpdateSettings(func(c *Settings) { c.Pool.Strategy = "least_inflight"; c.Pool.StickyTTLSec = 0 })
				ids := []string{}
				for i := range size {
					p := "workbuddy"
					if i%2 == 1 {
						p = "trae"
					}
					id := fmt.Sprint(i)
					ids = append(ids, id)
					s.UpsertAccount(&accounts.Account{ID: id, Provider: p, Scope: "cn", Enabled: true})
				}
				m, err := s.SaveUnifiedModel(config.UnifiedModel{Name: "shared-model-1", Enabled: true, Sources: []config.ModelSource{{ID: "workbuddy/cn/shared", Enabled: true}, {ID: "trae/cn/shared", Enabled: true}}}, nil, 0, s.UnifiedSnapshot().Revision, nil)
				if err != nil {
					b.Fatal(err)
				}
				g, err := s.SaveUnifiedGroup(AccountGroup{Name: "bench", AccountIDs: ids, UnifiedModels: []access.UnifiedGrant{{ModelID: m.ID, Visible: true, Callable: true, SourceMode: "all", SourceIDs: []string{}}}}, 0)
				if err != nil {
					b.Fatal(err)
				}
				key, err := s.AddKeyInGroupAndSave("bench", g.ID)
				if err != nil {
					b.Fatal(err)
				}
				selection := access.UnifiedSelection{ID: m.ID, Revision: m.Revision, RoutingRevision: s.UnifiedSnapshot().Revision}
				sources := []string{"workbuddy/cn/shared", "trae/cn/shared"}
				s.pool.now = func() time.Time { return time.Unix(100, 0) }
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					var a *accounts.Account
					var release func()
					var err error
					if mode == "unified" {
						a, _, release, err = s.AcquireUnifiedAccount("", m.Name, sources, nil, key.Key, selection)
					} else {
						a, _, release, err = s.AcquireMappedAccount("", m.Name, sources, nil, key.Key)
					}
					release()
					if a == nil || err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// Quantify optional M6 rule cost independently from the unchanged legacy path.
// This uses real admission, snapshot cloning, model selection and lease release.
func BenchmarkProductAdmissionRules(b *testing.B) {
	for _, count := range []int{0, 79, 512} {
		b.Run(fmt.Sprintf("rules=%d", count), func(b *testing.B) {
			s, err := Open(filepath.Join(b.TempDir(), "state.json"))
			if err != nil {
				b.Fatal(err)
			}
			s.UpsertAccount(&accounts.Account{ID: "wb", Provider: "workbuddy", Scope: "cn", Enabled: true})
			rules := []access.ModelRule{}
			off := false
			for i := 0; i < count; i++ {
				rules = append(rules, access.ModelRule{ID: fmt.Sprintf("workbuddy/cn/fixture-%d", i), Visible: &off})
			}
			g, err := s.SaveGroup(AccountGroup{Name: "bench", AccountIDs: []string{"wb"}, ModelRules: rules, ModelMappings: []ModelMapping{{Name: "deepseek-4.1-flash", Sources: []string{"workbuddy/cn/deepseek-v4.1-flash"}}}})
			if err != nil {
				b.Fatal(err)
			}
			k, err := s.AddKeyInGroupAndSave("bench", g.ID)
			if err != nil {
				b.Fatal(err)
			}
			s.pool.now = func() time.Time { return time.Unix(100, 0) }
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				a, _, release, err := s.AcquireMappedAccount("", "deepseek-4.1-flash", []string{"workbuddy/cn/deepseek-v4.1-flash"}, nil, k.Key)
				release()
				if err != nil || a == nil {
					b.Fatal(err)
				}
			}
		})
	}
}
