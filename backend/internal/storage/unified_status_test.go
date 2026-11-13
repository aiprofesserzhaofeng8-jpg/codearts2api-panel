package storage

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestSourceRuntimeStatusSeparatesSignalsAndRecoversWithoutMutation(t *testing.T) {
	s := testPool(t, 5)
	now := time.Now()
	s.pool.now = func() time.Time { return now }
	s.SetAccountPaused("1", true)
	s.SetAccountEnabled("2", false)
	s.MarkModelResult("3", "model-v1", false, "rate", 30)
	s.mu.Lock()
	s.pool.inFlight = map[string]int{"4": s.st.Settings.Pool.MaxInFlight}
	s.st.Logs = []LogEntry{{Model: "model-v1", Time: now.Unix(), Status: 429, ErrorKind: "rate_limited", Key: "secret-key", Err: "private body"}}
	s.mu.Unlock()
	id := "codearts/model-v1"
	before := s.PoolSnapshot()
	v := s.SourceRuntimeStatus([]string{id})[id]
	if v.AccountCount != 5 || v.Ready != 1 || v.Disabled != 1 || v.Paused != 1 || v.Cooling != 1 || v.Busy != 1 || v.NextRetryAt != now.Unix()+30 || v.LastRequest == nil || v.LastRequest.Status != 429 {
		t.Fatal(v)
	}
	if !reflect.DeepEqual(before, s.PoolSnapshot()) {
		t.Fatal("observation mutated scheduling")
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "private") {
		t.Fatal("private log leaked")
	}
	now = now.Add(time.Minute)
	v = s.SourceRuntimeStatus([]string{id})[id]
	if v.Ready != 2 || v.Cooling != 0 || v.Paused != 1 || v.Disabled != 1 {
		t.Fatal("cooldown did not expire independently", v)
	}
}

func TestSourceCooldownExpiryReentersAdmissionButNeverUnpauses(t *testing.T) {
	s := testPool(t, 1)
	now := time.Now()
	s.pool.now = func() time.Time { return now }
	s.MarkModelResult("0", "model-v1", false, "explicit retry time", 30)
	a, release, err := s.AcquireAccountForKey("", "model-v1", nil, "")
	release()
	if err != nil || a != nil {
		t.Fatal("cooling model admitted", a, err)
	}
	now = now.Add(31 * time.Second)
	a, release, err = s.AcquireAccountForKey("", "model-v1", nil, "")
	if err != nil || a == nil {
		t.Fatal("expired cooldown required reset", err)
	}
	release()
	release()
	s.SetAccountPaused("0", true)
	now = now.Add(time.Hour)
	a, release, err = s.AcquireAccountForKey("", "model-v1", nil, "")
	release()
	if a != nil || err != nil {
		t.Fatal("automatic recovery bypassed pause")
	}
	s.SetAccountPaused("0", false)
	s.SetAccountEnabled("0", false)
	a, release, err = s.AcquireAccountForKey("", "model-v1", nil, "")
	release()
	if a != nil || err != nil {
		t.Fatal("automatic recovery bypassed disable")
	}
	if s.PoolSnapshot()["0"].InFlight != 0 {
		t.Fatal("lease leaked")
	}
}
