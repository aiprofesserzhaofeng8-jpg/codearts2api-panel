package storage

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"codearts2api/internal/codearts"
)

func TestMappedAccountSharesLeasesStickyAndCooldown(t *testing.T) {
	s := testPool(t, 0)
	for _, p := range []string{"workbuddy", "trae"} {
		s.UpsertAccount(&codearts.Account{ID: p, Provider: p, Scope: "cn", Enabled: true, MaxInFlight: 1})
	}
	sources := []string{"workbuddy/cn/deepseek-v4.1-flash", "trae/cn/deepseek-v4.1-flash"}
	g, err := s.SaveGroup(AccountGroup{Name: "shared", AccountIDs: []string{"workbuddy", "trae"}, ModelMappings: []ModelMapping{{Name: "deepseek-4.1-flash", Sources: sources}}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddKeyInGroupAndSave("client", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, model, release, err := s.AcquireMappedAccount("session", "deepseek-4.1-flash", sources, nil, k.Key)
	if err != nil || a == nil || a.ProviderID() != "workbuddy" || model != sources[0] {
		t.Fatal("wrong source", a, model, err)
	}
	// A direct call shares this exact lease limit with mapped calls.
	if direct, r, e := s.AcquireAccountForKey("", sources[0], nil, k.Key); direct != nil || e != nil {
		r()
		t.Fatal("direct call bypassed concurrent lease")
	}
	b, model2, release2, err := s.AcquireMappedAccount("session", "deepseek-4.1-flash", sources, nil, k.Key)
	if err != nil || b == nil || b.ProviderID() != "trae" || model2 != sources[1] {
		t.Fatal("busy source did not switch", b, model2, err)
	}
	if none, _, r, e := s.AcquireMappedAccount("", "deepseek-4.1-flash", sources, nil, k.Key); none != nil || e != nil {
		r()
		t.Fatal("full pool leaked")
	}
	release()
	release()
	release2()
	again, _, r, e := s.AcquireMappedAccount("session", "deepseek-4.1-flash", sources, nil, k.Key)
	r()
	if e != nil || again.ID != b.ID {
		t.Fatal("sticky binding lost", e)
	}
	s.MarkModelResult(b.ID, model2, false, "busy", 600)
	again, actual, r, e := s.AcquireMappedAccount("session", "deepseek-4.1-flash", sources, nil, k.Key)
	r()
	if e != nil || again.ID != a.ID || actual != model {
		t.Fatal("model cooldown did not switch", e)
	}
	// An old request plan cannot resurrect a source removed by an admin.
	g.ModelMappings[0].Sources = []string{sources[1]}
	if _, err = s.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	if denied, _, r, e := s.AcquireMappedAccount("session", "deepseek-4.1-flash", []string{sources[0]}, nil, k.Key); denied != nil || e == nil {
		r()
		t.Fatal("stale plan bypass")
	}
}
func TestModelMappingsValidateAndPersist(t *testing.T) {
	s := testPool(t, 1)
	good := AccountGroup{Name: "versioned", AccountIDs: []string{"0"}, ModelMappings: []ModelMapping{{Name: "deepseek-4.1-flash", Sources: []string{"deepseek-v4.1-flash"}}}}
	for _, name := range []string{"deepseek", "deepseek/4.1", "deepseek 4.1", ""} {
		bad := cloneGroup(good)
		bad.ModelMappings[0].Name = name
		if _, err := s.SaveGroup(bad); err == nil {
			t.Fatal("invalid public name", name)
		}
	}
	bad := cloneGroup(good)
	bad.ModelMappings[0].Sources = append(bad.ModelMappings[0].Sources, "other-v2")
	if _, err := s.SaveGroup(bad); err == nil {
		t.Fatal("ambiguous provider source accepted")
	}
	bad = cloneGroup(good)
	bad.AllowlistEnabled = true
	if _, err := s.SaveGroup(bad); err == nil {
		t.Fatal("source outside allowlist accepted")
	}
	bad = cloneGroup(good)
	bad.AccountIDs = nil
	if _, err := s.SaveGroup(bad); err == nil {
		t.Fatal("source without account accepted")
	}
	s.UpdateSettings(func(c *Settings) { c.ForceModel = "deepseek-v4.1-flash" })
	if _, err := s.SaveGroup(good); err == nil {
		t.Fatal("forced model conflict accepted")
	}
	s.UpdateSettings(func(c *Settings) { c.ForceModel = "" })
	g, err := s.SaveGroup(good)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Groups()[0].ModelMappings, g.ModelMappings) {
		t.Fatal("mapping persistence lost")
	}
	copy := s.Groups()
	copy[0].ModelMappings[0].Sources[0] = "changed"
	if s.Groups()[0].ModelMappings[0].Sources[0] == "changed" {
		t.Fatal("mutable mapping snapshot")
	}
}

func BenchmarkModelSelection(b *testing.B) {
	for _, mode := range []string{"legacy", "raw_in_mapped_group", "mapped"} {
		b.Run(mode, func(b *testing.B) {
			s, err := Open(filepath.Join(b.TempDir(), "state.json"))
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < 13; i++ {
				s.UpsertAccount(&codearts.Account{ID: fmt.Sprint(i), Provider: "workbuddy", Scope: "cn", Enabled: true})
			}
			sources := []string{"workbuddy/cn/deepseek-v4.1-flash"}
			var mappings []ModelMapping
			if mode != "legacy" {
				mappings = []ModelMapping{{Name: "deepseek-4.1-flash", Sources: sources}}
			}
			g, err := s.SaveGroup(AccountGroup{Name: "bench", AccountIDs: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12"}, ModelMappings: mappings})
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
			for i := 0; i < b.N; i++ {
				if mode == "mapped" {
					a, _, r, e := s.AcquireMappedAccount("session", "deepseek-4.1-flash", sources, nil, k.Key)
					r()
					if a == nil || e != nil {
						b.Fatal(e)
					}
				} else {
					a, r, e := s.AcquireAccountForKey("session", sources[0], nil, k.Key)
					r()
					if a == nil || e != nil {
						b.Fatal(e)
					}
				}
			}
		})
	}
}
