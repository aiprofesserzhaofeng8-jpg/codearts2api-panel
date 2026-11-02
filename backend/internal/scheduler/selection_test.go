package scheduler

import "testing"

func TestSelectionStrategiesAndTies(t *testing.T) {
	for _, tc := range []struct {
		name, strategy       string
		cursor, chosen, next int
		loads                []int
		allowed              []bool
	}{
		{"round rotation", "round_robin", 1, 1, 2, []int{0, 1, 0}, []bool{true, true, true}},
		{"least load", "least_inflight", 1, 2, 0, []int{1, 2, 0}, []bool{true, true, true}},
		{"equal load rotates", "least_inflight", 2, 2, 0, []int{0, 0, 0}, []bool{true, true, true}},
		{"sequential retains", "sequential_exhaust", 1, 2, 2, []int{0, 0, 0}, []bool{true, false, true}},
		{"wrapped scan", "round_robin", 2, 0, 1, []int{0, 0, 0}, []bool{true, false, false}},
		{"no fallback", "least_inflight", 2, -1, 2, []int{0, 0, 0}, []bool{false, false, false}},
		{"empty", "round_robin", 0, -1, 0, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[int]bool{}
			chosen, next := Select(len(tc.loads), tc.cursor, tc.strategy, func(i int) (bool, int) {
				if seen[i] {
					t.Fatal("candidate visited twice")
				}
				seen[i] = true
				return tc.allowed[i], tc.loads[i]
			})
			if chosen != tc.chosen || next != tc.next {
				t.Fatalf("got %d/%d want %d/%d", chosen, next, tc.chosen, tc.next)
			}
		})
	}
}

func TestLegacyFallbackRemainsSeparate(t *testing.T) {
	chosen, next := Legacy(3, 1, func(i int) (bool, int) { return i != 2, []int{2, 4, 0}[i] })
	if chosen != 0 || next != 2 {
		t.Fatalf("fallback %d/%d", chosen, next)
	}
	chosen, next = Legacy(3, 1, func(int) (bool, int) { return false, 0 })
	if chosen != -1 || next != 2 {
		t.Fatalf("empty fallback %d/%d", chosen, next)
	}
	chosen, next = Legacy(0, 0, func(int) (bool, int) { t.Fatal("empty scan"); return false, 0 })
	if chosen != -1 || next != 0 {
		t.Fatal(chosen, next)
	}
	for _, tc := range []struct{ override, want int }{{-1, 2}, {0, 2}, {1, 1}, {32, 32}, {33, 2}} {
		if got := MaxInFlight(tc.override, 2); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
