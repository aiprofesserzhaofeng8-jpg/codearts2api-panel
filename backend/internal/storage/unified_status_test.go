package storage

import (
	"reflect"
	"testing"
)

func TestUnifiedSourceCountsReflectHealthAndConcurrencyWithoutLeasing(t *testing.T) {
	s := testPool(t, 2)
	id := "codearts/model-v1"
	before := s.PoolSnapshot()
	counts, observed := s.UnifiedSourceCounts([]string{id, "trae/cn/model-v1"})
	if counts[id] != 2 || counts["trae/cn/model-v1"] != 0 || observed.IsZero() || !reflect.DeepEqual(before, s.PoolSnapshot()) {
		t.Fatal(counts, observed)
	}
	s.SetAccountPaused("1", true)
	s.MarkModelResult("0", "model-v1", false, "fixture", 60)
	counts, _ = s.UnifiedSourceCounts([]string{id})
	if counts[id] != 0 {
		t.Fatal("cooling or paused account counted", counts)
	}
	s.MarkModelResult("0", "model-v1", true, "", 0)
	max := s.Settings().Pool.MaxInFlight
	releases := []func(){}
	for i := 0; i < max; i++ {
		a, release, err := s.AcquireAccountForKey("", "model-v1", nil, "")
		if err != nil || a == nil {
			t.Fatal(a, err)
		}
		releases = append(releases, release)
	}
	counts, _ = s.UnifiedSourceCounts([]string{id})
	if counts[id] != 0 {
		t.Fatal("saturated account counted", counts)
	}
	for _, release := range releases {
		release()
	}
	counts, _ = s.UnifiedSourceCounts([]string{id})
	if counts[id] != 1 {
		t.Fatal("release not reflected", counts)
	}
}
